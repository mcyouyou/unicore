package plugin

import (
	"context"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	LabelGangName           = "gang.unicore.mcyou.cn/name"
	LabelGangAvailableCount = "gang.unicore.mcyou.cn/available-count"
)

type GangScheduler struct {
	podLister             listerv1.PodLister
	handle                framework.Handle
	firstGangPodCreatedAt sync.Map
	mu                    sync.Mutex
}

var _ framework.PermitPlugin = &GangScheduler{}
var _ framework.QueueSortPlugin = &GangScheduler{}
var _ framework.PreFilterPlugin = &GangScheduler{}

func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &GangScheduler{
		podLister: handle.SharedInformerFactory().Core().V1().Pods().Lister(),
		handle:    handle,
	}, nil
}

func (g GangScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

func (g GangScheduler) PreFilter(ctx context.Context, state *framework.CycleState, p *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	g.handle.
}

// Less imply QueueSort plugin
func (g GangScheduler) Less(pod1 *framework.QueuedPodInfo, pod2 *framework.QueuedPodInfo) bool {
	gang1, gang2 := pod1.Pod.Labels[LabelGangName], pod2.Pod.Labels[LabelGangName]
	time1, time2 := pod1.Pod.CreationTimestamp.Time, pod2.Pod.CreationTimestamp.Time
	if gang1 != "" {
		fistGangPodTime, ok := g.firstGangPodCreatedAt.Load(gang1)
		if ok {
			time1 = fistGangPodTime.(time.Time)
		} else {
			g.firstGangPodCreatedAt.Store(gang1, time1)
		}
	}
	if gang2 != "" {
		fistGangPodTime, ok := g.firstGangPodCreatedAt.Load(gang2)
		if ok {
			time2 = fistGangPodTime.(time.Time)
		} else {
			g.firstGangPodCreatedAt.Store(gang2, time2)
		}
	}

	if time1.Equal(time2) && gang1 != "" && gang2 != "" {
		// compare by gang name for gangs created at the same time
		return strings.Compare(gang1, gang2) < 0
	}
	return time1.Before(time2)
}

// Permit imply Permit plugin
func (g GangScheduler) Permit(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) (*framework.Status, time.Duration) {
	gangName, availableCount := p.Labels[LabelGangName], p.Labels[LabelGangAvailableCount]
	if gangName == "" || availableCount == "" {
		return framework.NewStatus(framework.Success, "not gang-pod"), 0
	}
	runningPods, err := g.getRunningPodsInGang(gangName)
	if err != nil {
		klog.Errorf("PERMIT list running pod for gang %v err: %v", gangName, err)
		return framework.NewStatus(framework.Error, err.Error()), 0
	}
	availableCnt, err := strconv.Atoi(availableCount)
	if err != nil {
		klog.Errorf("PERMIT parse availableCnt for pod %v err: %v", p.Name, err)
		return framework.NewStatus(framework.Error, err.Error()), 0
	}
	waitingPods := g.getWaitingPodsInGang(gangName)
	if len(waitingPods)+len(runningPods)+1 < availableCnt {
		return framework.NewStatus(framework.Wait, fmt.Sprintf("waiting for pod count to reach availableCnt %v: %v waiting, %v running",
			availableCnt, len(waitingPods), len(runningPods))), time.Minute
	}

	// permit the entire gang
	g.mu.Lock()
	defer g.mu.Unlock()
	g.handle.IterateOverWaitingPods(func(pod framework.WaitingPod) {
		if pod.GetPod().Labels[LabelGangName] == gangName {
			pod.Allow(g.Name())
		}
	})
	g.firstGangPodCreatedAt.Delete(gangName)
	return framework.NewStatus(framework.Success, "gang availableCnt meet: "+fmt.Sprintf("%d", len(waitingPods)+len(runningPods)+1)), 0
}

func (g GangScheduler) Name() string {
	return "GangScheduler"
}

func (g GangScheduler) getWaitingPodsInGang(gangName string) []*v1.Pod {
	ret := make([]*v1.Pod, 0)
	g.handle.IterateOverWaitingPods(func(pod framework.WaitingPod) {
		if pod.GetPod().Labels[LabelGangName] == gangName {
			ret = append(ret, pod.GetPod())
		}
	})
	return ret
}

func (g GangScheduler) getRunningPodsInGang(gangName string) ([]*v1.Pod, error) {
	pods, err := g.podLister.List(labels.SelectorFromSet(map[string]string{LabelGangName: gangName}))
	if err != nil {
		return nil, err
	}
	ret := make([]*v1.Pod, 0)
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodRunning {
			ret = append(ret, pod)
		}
	}
	return ret, nil
}
