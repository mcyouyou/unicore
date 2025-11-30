package reserve

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const ReserveConfigMapName = "reservation"
const ReserveConfigMapNamespace = "unicore"
const ExpireCheckLoopTime = time.Second * 30

type Reservation struct {
	ReservingPod          string    `json:"pod"`
	ReservingPodNamespace string    `json:"namespace"`
	CPUMilli              int64     `json:"cpu"`
	MemMilli              int64     `json:"mem"`
	ExpireAt              time.Time `json:"expire"`
}

type ReserveScheduler struct {
	// map[nodeName][]Reservation
	reservation map[string][]Reservation
	latestCM    *v1.ConfigMap
	podLister   listerv1.PodLister
	cmInformer  cache.SharedIndexInformer
	handle      framework.Handle
	mu          sync.RWMutex
}

var _ framework.FilterPlugin = &ReserveScheduler{}
var _ framework.ReservePlugin = &ReserveScheduler{}

func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	klog.Info("starting unicore-reserve-scheduler..")

	r := &ReserveScheduler{
		podLister: handle.SharedInformerFactory().Core().V1().Pods().Lister(),
		handle:    handle,
	}

	r.cmInformer = handle.SharedInformerFactory().Core().V1().ConfigMaps().Informer()
	_, err := r.cmInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{

		AddFunc: func(obj interface{}) {
			cm := obj.(*v1.ConfigMap)
			if cm.Name == ReserveConfigMapName && cm.Namespace == ReserveConfigMapNamespace {
				r.mu.Lock()
				defer r.mu.Unlock()
				err := r.loadFromConfigMap(cm.Data)
				if err != nil {
					klog.Errorf("load reservation from cm err: %v", err)
				}
				r.latestCM = cm
				klog.Infof("watch: reserve configMap added")
			}
		},

		UpdateFunc: func(oldObj, newObj interface{}) {
			newCM := newObj.(*v1.ConfigMap)

			if newCM.Name == ReserveConfigMapName && newCM.Namespace == ReserveConfigMapNamespace {
				r.mu.Lock()
				defer r.mu.Unlock()
				err := r.loadFromConfigMap(newCM.Data)
				if err != nil {
					klog.Errorf("load reservation from cm err: %v", err)
				}
				r.latestCM = newCM
				klog.Infof("watch: reserve configMap modified, new reservations: %+v", r.reservation)
			}
		},

		DeleteFunc: func(obj interface{}) {
			cm := obj.(*v1.ConfigMap)
			if cm.Name == ReserveConfigMapName && cm.Namespace == ReserveConfigMapNamespace {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.reservation = make(map[string][]Reservation)
				r.latestCM = nil
				klog.Infof("watch: reserve configMap deleted")
			}
		},
	})
	if err != nil {
		return nil, err
	}

	stopCh := make(chan struct{})
	go r.cmInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, r.cmInformer.HasSynced) {
		panic("cm informer sync failed")
	}

	go r.cleanExpired()
	return r, nil
}

func (r *ReserveScheduler) Reserve(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) *framework.Status {
	var newCM *v1.ConfigMap
	newReservation := make(map[string][]Reservation)
	updated := false
	r.mu.Lock()
	if r.latestCM == nil {
		r.mu.Unlock()
		klog.Errorf("latest cm is nil, skipping")
		return framework.NewStatus(framework.Success)
	}
	newCM = r.latestCM.DeepCopy()
	for nodeName, reservations := range r.reservation {
		newReservation[nodeName] = make([]Reservation, 0)
		for _, reservation := range reservations {
			if !(reservation.ReservingPod == p.Name && reservation.ReservingPodNamespace == p.Namespace) {
				newReservation[nodeName] = append(newReservation[nodeName], reservation)
			} else {
				updated = true
			}
		}
	}
	if updated {
		r.reservation = newReservation
	}
	data, err := r.dumpToConfigMap()
	r.mu.Unlock()
	if err != nil {
		klog.Errorf("dump reservation to cm data err: %v", err)
		return framework.NewStatus(framework.Success)
	}

	if updated {
		newCM.Data = data
		_, err := r.handle.ClientSet().CoreV1().ConfigMaps(ReserveConfigMapNamespace).Update(context.TODO(), newCM, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("update reserve cm err: %v", err)
		}
	}
	return framework.NewStatus(framework.Success)
}

func (r *ReserveScheduler) Unreserve(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) {
}

func (r *ReserveScheduler) Filter(ctx context.Context, state *framework.CycleState, pod *v1.Pod, nodeInfo *framework.NodeInfo) *framework.Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	reservations := r.reservation[nodeInfo.Node().Name]
	for _, reservation := range reservations {
		if time.Now().After(reservation.ExpireAt) {
			continue
		}
		if !(reservation.ReservingPod == pod.Name && reservation.ReservingPodNamespace == pod.Namespace) {
			freeCPU := nodeInfo.Allocatable.MilliCPU - nodeInfo.Requested.MilliCPU
			freeMem := nodeInfo.Allocatable.Memory - nodeInfo.Requested.Memory

			needCPU, needMem := int64(0), int64(0)
			for _, container := range pod.Spec.Containers {
				needCPU += container.Resources.Requests.Cpu().MilliValue()
				needMem += container.Resources.Requests.Memory().Value()
			}
			if freeCPU < needCPU || freeMem < needMem {
				klog.Infof("reserved resource used by pod %v in namespace %s, filtering coming pod %s/%v for "+
					"freeCPU %v < needCPU %v || freeMem %v < needMem %v", reservation.ReservingPod,
					reservation.ReservingPodNamespace, pod.Namespace, pod.Name, freeCPU, needCPU, freeMem, needMem)
				return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("reserved resource used by pod %v "+
					"in namespace %s", reservation.ReservingPod, reservation.ReservingPodNamespace))
			}
		}
	}
	return framework.NewStatus(framework.Success)
}

func (r *ReserveScheduler) cleanExpired() {
	for {
		time.Sleep(ExpireCheckLoopTime)
		r.mu.RLock()
		newCM := r.latestCM.DeepCopy()
		newReservation := make(map[string][]Reservation)
		updated := false
		for nodeName, reservations := range r.reservation {
			newReservation[nodeName] = make([]Reservation, 0)
			for _, reservation := range reservations {
				if reservation.ExpireAt.After(time.Now()) {
					newReservation[nodeName] = append(newReservation[nodeName], reservation)
				} else {
					updated = true
				}
			}
		}
		if !updated {
			r.mu.Unlock()
			continue
		}
		r.reservation = newReservation
		data, err := r.dumpToConfigMap()
		if err != nil {
			klog.Errorf("dump reservation to cm data err: %v", err)
			r.mu.Unlock()
			continue
		}

		newCM.Data = data
		_, err = r.handle.ClientSet().CoreV1().ConfigMaps(ReserveConfigMapNamespace).Update(context.TODO(), newCM, metav1.UpdateOptions{})
		if err != nil {
			klog.Errorf("update reserve cm err: %v", err)
		}
		r.mu.RUnlock()
	}
}

func (r *ReserveScheduler) loadFromConfigMap(data map[string]string) error {
	r.reservation = make(map[string][]Reservation)

	for node, raw := range data {
		var list []Reservation
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			return fmt.Errorf("unmarshal node %s: %w", node, err)
		}
		r.reservation[node] = list
	}

	return nil
}

func (r *ReserveScheduler) dumpToConfigMap() (map[string]string, error) {
	out := make(map[string]string)

	for node, list := range r.reservation {
		b, err := json.Marshal(list)
		if err != nil {
			return nil, fmt.Errorf("marshal node %s: %w", node, err)
		}
		out[node] = string(b)
	}

	return out, nil
}

func (r *ReserveScheduler) Name() string {
	return "unicore-reserve-scheduler"
}
