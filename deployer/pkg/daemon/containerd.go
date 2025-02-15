package daemon

import (
	"context"
	"fmt"
	"k8s.io/klog/v2"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/namespaces"
)

const (
	socketPath = "/run/containerd/containerd.sock"
	timeout    = 10 * time.Second
	namespace  = "k8s.io"
)

var validImageRegex = regexp.MustCompile(`^(?:(?:[a-zA-Z0-9.-]+(?:\:[0-9]+)?/)*[a-z0-9]+(?:[._-][a-z0-9]+)*)(?::[\w.-]+)?(?:@sha256:[a-fA-F0-9]{64})?$`)

func IsValidDockerImageName(imageName string) bool {
	return validImageRegex.MatchString(imageName)
}

type ContainerDCRI struct {
	ContainerDCli *containerd.Client
}

func NewContainerDCRI() (*ContainerDCRI, error) {
	cli, err := containerd.New(socketPath,
		containerd.WithTimeout(timeout),
		containerd.WithDefaultNamespace(namespace),
	)
	if err != nil {
		return nil, err
	}
	return &ContainerDCRI{ContainerDCli: cli}, nil
}

func (c *ContainerDCRI) CheckAndPull(imageList []string) (map[string]bool, error) {
	ctx := namespaces.WithNamespace(context.Background(), namespace)

	existingImages, err := getExistingImages(ctx, c.ContainerDCli)
	if err != nil {
		klog.Errorf("failed to get existing images: %v", err)
		return nil, err
	}

	toPull := filterMissingImages(imageList, existingImages)
	if len(toPull) == 0 {
		klog.Infof("no images found to pull, skipping")
		return nil, nil
	}

	klog.Infof("pulling %v images...", len(toPull))
	var wg sync.WaitGroup
	var mu sync.Mutex
	failedPull := make(map[string]bool)
	for _, img := range toPull {
		wg.Add(1)
		go func() {
			if err := pullSingleImage(ctx, c.ContainerDCli, img); err != nil {
				klog.Errorf("failed to pull image %v: %v", img, err)
				mu.Lock()
				failedPull[img] = true
				mu.Unlock()
			} else {
				klog.Infof("image pulled: %v", img)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	return failedPull, nil
}

func getExistingImages(ctx context.Context, client *containerd.Client) (map[string]struct{}, error) {
	imageService := client.ImageService()
	images, err := imageService.List(ctx)
	if err != nil {
		return nil, err
	}

	existing := make(map[string]struct{})
	for _, img := range images {
		existing[img.Name] = struct{}{}
	}
	return existing, nil
}

func filterMissingImages(targets []string, existing map[string]struct{}) []string {
	var missing []string
	for _, img := range targets {
		if _, ok := existing[img]; !ok {
			missing = append(missing, img)
		}
	}
	return missing
}

func pullSingleImage(ctx context.Context, client *containerd.Client, image string) error {
	if !IsValidDockerImageName(image) {
		return fmt.Errorf("invalid image name: %v", image)
	}
	if !strings.Contains(image, "/") {
		image = "docker.io/library/" + image
	}
	if !strings.Contains(image, ":") {
		image += ":latest"
	}
	_, err := client.Pull(ctx, image,
		containerd.WithPullUnpack,
		containerd.WithPullLabel("source", "unicore-daemon"),
	)
	return err
}
