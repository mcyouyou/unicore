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
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const ReserveConfigMapName = "reservation"
const ReserveConfigMapNamespace = "unicore"
const ExpireCheckLoopTime = time.Second * 30

type Reservation struct {
	ReservingPod          string `json:"pod"`
	ReservingPodNamespace string `json:"namespace"`
	CPUMilli              int64  `json:"cpu"`
	MemMilli              int64  `json:"mem"`
	ExpireAt              int64  `json:"expire"`
}

type ReserveScheduler struct {
	// map[nodeName][]Reservation
	// readonly cache maintained by informer, protected by the mutex
	reservation map[string][]Reservation
	mu          sync.RWMutex

	podLister  listerv1.PodLister
	cmLister   listerv1.ConfigMapLister
	cmInformer cache.SharedIndexInformer
	handle     framework.Handle
}

var _ framework.FilterPlugin = &ReserveScheduler{}
var _ framework.ReservePlugin = &ReserveScheduler{}

func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	klog.Info("starting unicore-reserve-scheduler..")

	r := &ReserveScheduler{
		podLister:   handle.SharedInformerFactory().Core().V1().Pods().Lister(),
		cmLister:    handle.SharedInformerFactory().Core().V1().ConfigMaps().Lister(),
		handle:      handle,
		reservation: make(map[string][]Reservation),
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
				klog.Infof("watch: reserve configMap modified, new reservations: %+v", r.reservation)
			}
		},

		DeleteFunc: func(obj interface{}) {
			cm := obj.(*v1.ConfigMap)
			if cm.Name == ReserveConfigMapName && cm.Namespace == ReserveConfigMapNamespace {
				r.mu.Lock()
				defer r.mu.Unlock()
				r.reservation = make(map[string][]Reservation)
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
	newReservation := make(map[string][]Reservation)
	updated := false

	r.mu.RLock()
	for nodeName, reservations := range r.reservation {
		newReservation[nodeName] = make([]Reservation, 0)
		for _, reservation := range reservations {
			if !(reservation.ReservingPod == p.Name && reservation.ReservingPodNamespace == p.Namespace) {
				newReservation[nodeName] = append(newReservation[nodeName], reservation)
			} else {
				klog.Infof("reserving pod %s was scheduleed to node %s, removing from cm..", p.Name, nodeName)
				updated = true
			}
		}
	}
	r.mu.RUnlock()

	if updated {
		err := r.updateCM(newReservation)
		if err != nil {
			klog.Errorf("update reservation for pod %s/%s err: %v", p.Namespace, p.Name, err)
			return framework.NewStatus(framework.Success)
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
	if len(reservations) > 0 {
		klog.Infof("filtering node %s has %d reservations", nodeInfo.Node().Name, len(reservations))
	}

	freeCPU := nodeInfo.Allocatable.MilliCPU - nodeInfo.Requested.MilliCPU
	freeMem := nodeInfo.Allocatable.Memory - nodeInfo.Requested.Memory
	reservedCPU, reservedMem := int64(0), int64(0)
	needCPU, needMem := int64(0), int64(0)
	for _, container := range pod.Spec.Containers {
		needCPU += container.Resources.Requests.Cpu().MilliValue()
		// mem.Value 返回的为字节，MilliValue 无意义
		needMem += container.Resources.Requests.Memory().Value()
	}
	klog.Infof("handling Filter for pod %s/%s on node %s, needCPU: %v, needMem: %v", pod.Namespace, pod.Name, nodeInfo.Node().Name, needCPU, needMem)

	for _, reservation := range reservations {
		if time.Now().After(time.Unix(reservation.ExpireAt, 0)) {
			klog.Infof("pod %s's reservation expired, skipping", reservation.ReservingPod)
			continue
		}
		reservedCPU += reservation.CPUMilli
		reservedMem += reservation.MemMilli
		if !(reservation.ReservingPod == pod.Name && reservation.ReservingPodNamespace == pod.Namespace) {
			if freeCPU-reservedCPU < needCPU || freeMem-reservedMem < needMem {
				klog.Infof("free resource not enough after reservation for pod %s/%s: freeCPU %v - reservedCPU %v < needed %v OR"+
					" freeMem %v - reservedMem %v < needed %v, reservations: %+v",
					pod.Namespace, pod.Name, freeCPU, reservedCPU, needCPU, freeMem, reservedMem, needMem, reservations)
				return framework.NewStatus(framework.Unschedulable, fmt.Sprintf("free resource not enough after reservation:"+
					" freeCPU %v - reservedCPU %v < needed %v OR"+
					" freeMem %v - reservedMem %v < needed %v", freeCPU, reservedCPU, needCPU, freeMem, reservedMem, needMem))
			}
		}
	}
	return framework.NewStatus(framework.Success)
}

func (r *ReserveScheduler) cleanExpired() {
	for {
		time.Sleep(ExpireCheckLoopTime)
		r.mu.RLock()
		newReservation := make(map[string][]Reservation)
		updated := false
		for nodeName, reservations := range r.reservation {
			newReservation[nodeName] = make([]Reservation, 0)
			for _, reservation := range reservations {
				if reservation.ExpireAt > time.Now().Unix() {
					newReservation[nodeName] = append(newReservation[nodeName], reservation)
				} else {
					updated = true
				}
			}
		}
		r.mu.RUnlock()
		if !updated {
			continue
		}
		err := r.updateCM(newReservation)
		if err != nil {
			klog.Errorf("update cm err, skip cleaning expired: %v", err)
		}
	}
}

// update cm to cluster
func (r *ReserveScheduler) updateCM(newReservation map[string][]Reservation) error {
	var cm *v1.ConfigMap
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		cm, err = r.cmLister.ConfigMaps(ReserveConfigMapNamespace).Get(ReserveConfigMapName)
		if err != nil {
			return err
		}
		data, err := r.dumpToConfigMap(newReservation)
		if err != nil {
			klog.Errorf("dump reservation to cm data err: %v", err)
			return err
		}
		cm.Data = data
		_, err = r.handle.ClientSet().CoreV1().ConfigMaps(ReserveConfigMapNamespace).Update(context.TODO(), cm, metav1.UpdateOptions{})
		return err
	})
	return err
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

func (r *ReserveScheduler) dumpToConfigMap(reservation map[string][]Reservation) (map[string]string, error) {
	out := make(map[string]string)

	for node, list := range reservation {
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
