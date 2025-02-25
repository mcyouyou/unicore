package daemon

import (
	"context"
	"fmt"
	unicore "github.com/mcyouyou/unicore/api/deployer/v1"
	"github.com/mcyouyou/unicore/pkg/generated/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"os"
	"sync"
	"time"
)

type Daemon struct {
	Config   *rest.Config
	KubeCli  *kubernetes.Clientset
	Cli      *versioned.Clientset
	NodeName string
	// map[ImgName]NextPullTime
	AlwaysPullImages     map[string]metav1.Time
	AlwaysPullImagesLock sync.RWMutex
	RetryImages          map[string]bool
	RetryLock            sync.RWMutex
	Watcher              watch.Interface

	CRI CRI
}

func New() (*Daemon, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	d := &Daemon{
		Config:           config,
		NodeName:         os.Getenv("NODE_NAME"),
		AlwaysPullImages: make(map[string]metav1.Time),
		RetryImages:      make(map[string]bool),
	}
	d.KubeCli, err = kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	d.Cli, err = versioned.NewForConfig(d.Config)
	if err != nil {
		return nil, err
	}
	cri, err := NewContainerDCRI()
	if err != nil {
		return nil, err
	}
	d.CRI = cri
	return d, nil
}

// Start watching node imageList change event
func (d *Daemon) Start() error {
	listOptions := metav1.ListOptions{
		FieldSelector: fmt.Sprintf("metadata.name=%s", d.NodeName),
	}
	list, err := d.Cli.UnicoreV1().ImageLists(unicore.Namespace).List(context.TODO(), listOptions)
	if err != nil {
		return err
	}
	watchOptions := metav1.ListOptions{}
	if len(list.Items) != 0 {
		var gotItem unicore.ImageList
		for _, item := range list.Items {
			gotItem = item
		}
		err = d.PullImages(gotItem.Spec.Images)
		if err != nil {
			klog.ErrorS(err, "pull image failed", "image", gotItem.Spec.Images)
		}
		resourceVersion := list.GetResourceVersion()
		watchOptions = metav1.ListOptions{
			FieldSelector:   fmt.Sprintf("metadata.name=%s", d.NodeName),
			ResourceVersion: resourceVersion, // watch from the list-returned version
		}
	} else {
		watchOptions = metav1.ListOptions{FieldSelector: fmt.Sprintf("metadata.name=%s", d.NodeName)}
	}

	watcher, err := d.Cli.UnicoreV1().ImageLists(unicore.Namespace).Watch(context.TODO(), watchOptions)
	if err != nil {
		return err
	}

	klog.Infof("watching imageList changes on node %s", d.NodeName)
	go d.loopAlwaysPull()
	for event := range watcher.ResultChan() {
		switch event.Type {
		case watch.Added:
			imageList := event.Object.(*unicore.ImageList)
			err := d.PullImages(imageList.Spec.Images)
			if err != nil {
				klog.ErrorS(err, "pull image failed", "image", imageList.Spec.Images)
			}
		case watch.Modified:
			imageList := event.Object.(*unicore.ImageList)
			err := d.PullImages(imageList.Spec.Images)
			if err != nil {
				klog.ErrorS(err, "pull image failed", "image", imageList.Spec.Images)
			}
		case watch.Deleted:
			d.AlwaysPullImagesLock.Lock()
			d.AlwaysPullImages = make(map[string]metav1.Time)
			d.AlwaysPullImagesLock.Unlock()
			d.RetryLock.Lock()
			d.RetryImages = make(map[string]bool)
			d.RetryLock.Unlock()
		}
	}
	return nil
}

func (d *Daemon) PullImages(imageList map[string]unicore.ImageInfo) error {
	toPull := make([]string, 0)
	for imgName, imgInfo := range imageList {
		toPull = append(toPull, imgName)
		if imgInfo.AlwaysPull {
			d.AlwaysPullImagesLock.Lock()
			d.AlwaysPullImages[imgName] = imgInfo.NextPull
			d.AlwaysPullImagesLock.Unlock()
		}
	}
	failed, err := d.CRI.CheckAndPull(toPull)
	if err != nil {
		return err
	}
	if len(failed) > 0 {
		d.RetryLock.Lock()
		defer d.RetryLock.Unlock()
		for k, v := range failed {
			if v {
				d.RetryImages[k] = true
			}
		}
	}
	return nil
}

func (d *Daemon) loopRetry() {
	for {
		d.RetryLock.RLock()
		toPull := make([]string, 0)
		for k, v := range d.RetryImages {
			if v {
				toPull = append(toPull, k)
			}
		}
		d.RetryLock.RUnlock()
		if len(toPull) > 0 {
			go func() {
				failedImages, err := d.CRI.CheckAndPull(toPull)
				if err != nil {
					klog.ErrorS(err, "pull image failed", "image", toPull)
				} else {
					for _, v := range toPull {
						if !failedImages[v] {
							d.RetryLock.Lock()
							delete(d.RetryImages, v)
							d.RetryLock.Unlock()
						}
					}
				}
			}()
		}
		time.Sleep(30 * time.Second)
	}
}

func (d *Daemon) loopAlwaysPull() {
	for {
		d.AlwaysPullImagesLock.RLock()
		toPull := make([]string, 0)
		for k, v := range d.AlwaysPullImages {
			if v.Time.Before(time.Now()) {
				toPull = append(toPull, k)
			}
		}
		d.AlwaysPullImagesLock.RUnlock()
		if len(toPull) > 0 {
			go func() {
				failedImages, err := d.CRI.CheckAndPull(toPull)
				if err != nil {
					klog.ErrorS(err, "pull image failed", "image", toPull)
				} else {
					d.AlwaysPullImagesLock.Lock()
					defer d.AlwaysPullImagesLock.Unlock()
					for _, v := range toPull {
						if failedImages[v] {
							d.RetryLock.Lock()
							d.RetryImages[v] = true
							d.RetryLock.Unlock()
						} else {
							d.AlwaysPullImages[v] = metav1.NewTime(time.Now().Add(24 * time.Hour))
						}
					}
				}
			}()
		}
		time.Sleep(5 * time.Minute)
	}
}
