package gang

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/scheduler/framework"
)

const (
	LabelGangName           = "gang.unicore.mcyou.cn/name"
	LabelGangAvailableCount = "gang.unicore.mcyou.cn/available-count"
	GangExpirationTime      = time.Minute * 5
)

type GangScheduler struct {
	podLister               listerv1.PodLister
	handle                  framework.Handle
	firstGangPodCreatedAt   sync.Map
	firstGangPodPermittedAt sync.Map
	permittedGangPods       sync.Map
	rejectedGangs           sync.Map
	mu                      sync.Mutex
}

var _ framework.PermitPlugin = &GangScheduler{}
var _ framework.QueueSortPlugin = &GangScheduler{}
var _ framework.PreFilterPlugin = &GangScheduler{}
var _ framework.ReservePlugin = &GangScheduler{}

func New(_ context.Context, _ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	klog.Info("starting unicore-gang-scheduler..")
	g := &GangScheduler{
		podLister: handle.SharedInformerFactory().Core().V1().Pods().Lister(),
		handle:    handle,
	}
	go g.CleanUp()
	return g, nil
}

// CleanUp clean up expired gang's states
func (g *GangScheduler) CleanUp() {
	for {
		time.Sleep(time.Second)
		g.mu.Lock()
		g.firstGangPodPermittedAt.Range(func(key, value any) bool {
			if !value.(time.Time).IsZero() && value.(time.Time).Add(GangExpirationTime).Before(time.Now()) {
				gangName := key.(string)
				g.firstGangPodPermittedAt.Delete(gangName)
				g.firstGangPodCreatedAt.Delete(gangName)
				g.permittedGangPods.Delete(gangName)
				g.rejectedGangs.Delete(gangName)
				klog.Infof("cleaned expired gang %v's states", gangName)
			}
			return true
		})
		g.mu.Unlock()
	}
}

func (g *GangScheduler) Reserve(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) *framework.Status {
	return framework.NewStatus(framework.Success)
}

// Unreserve imply Reserve plugin
func (g *GangScheduler) Unreserve(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) {
	gangName, _ := p.Labels[LabelGangName], p.Labels[LabelGangAvailableCount]
	if gangName == "" {
		return
	}

	klog.Infof("recieved unreserve event for gang %v's pod %v", gangName, p.Name)

	// reject the entire gang
	g.mu.Lock()
	defer g.mu.Unlock()
	rejectCnt := 0
	g.handle.IterateOverWaitingPods(func(pod framework.WaitingPod) {
		if pod.GetPod().Labels[LabelGangName] == gangName {
			pod.Reject(g.Name(), fmt.Sprintf("gang's pod %s is unreserved", p.Name))
			rejectCnt++
		}
	})
	g.rejectedGangs.Store(gangName, true)
	if rejectCnt > 0 {
		klog.Infof("Unreserve rejected %v pods for gang %v, trigger pod: %v", rejectCnt, gangName, p.Name)
	}
}

func (g *GangScheduler) PreFilterExtensions() framework.PreFilterExtensions {
	return nil
}

// PreFilter imply PreFilter plugin
func (g *GangScheduler) PreFilter(ctx context.Context, state *framework.CycleState, p *v1.Pod) (*framework.PreFilterResult, *framework.Status) {
	gangName, availableCount := p.Labels[LabelGangName], p.Labels[LabelGangAvailableCount]
	if gangName == "" || availableCount == "" {
		return nil, framework.NewStatus(framework.Success, "not gang-pod")
	}

	_, ok := g.rejectedGangs.Load(gangName)
	if ok {
		return nil, framework.NewStatus(framework.UnschedulableAndUnresolvable, "gang "+gangName+" has been rejected")
	}
	klog.Infof("gang %v's pod %v entering PreFilter state", gangName, p.Name)

	runningPods, pendingPods, err := g.getRunningAndPendingPodsInGang(gangName)
	if err != nil {
		klog.Errorf("PreFilter list running pod for gang %v err: %v", gangName, err)
		return nil, framework.NewStatus(framework.Error, err.Error())
	}

	availableCnt, err := strconv.Atoi(availableCount)
	if err != nil {
		klog.Errorf("PreFilter parse availableCnt for pod %v err: %v", p.Name, err)
		return nil, framework.NewStatus(framework.Error, err.Error())
	}
	if len(runningPods)+len(pendingPods) < availableCnt {
		klog.Errorf("PreFilter not enough available pod for gang %v, filter out pod %v, %v running + %v pending"+
			" < %v available needed",
			gangName, p.Name, len(runningPods), len(pendingPods), availableCnt)
		return nil, framework.NewStatus(framework.Unschedulable, fmt.Sprintf("not enough available pod for gang %v", gangName))
	}
	klog.Infof("gang %v's pod %v passed prefilter for %v running + %v pending >= %v available needed",
		gangName, p.Name, len(runningPods), len(pendingPods), availableCnt)
	return nil, framework.NewStatus(framework.Success, fmt.Sprintf("gang availableCnt meet: %v", len(runningPods)+len(pendingPods)))
}

// Less imply QueueSort plugin
func (g *GangScheduler) Less(pod1 *framework.QueuedPodInfo, pod2 *framework.QueuedPodInfo) bool {
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
		klog.Infof("Less comparing names for gang %v's pod %v and gang %v's pod %v: %v",
			gang1, pod1.Pod.Name, gang2, pod2.Pod.Name, strings.Compare(gang1, gang2) < 0)
		return strings.Compare(gang1, gang2) < 0
	}
	klog.Infof("Less comparing time for gang %v's pod %v and gang %v's pod %v: %v and %v",
		gang1, pod1.Pod.Name, gang2, pod2.Pod.Name, time1, time2)
	return time1.Before(time2)
}

// Permit imply Permit plugin
func (g *GangScheduler) Permit(ctx context.Context, state *framework.CycleState, p *v1.Pod, nodeName string) (*framework.Status, time.Duration) {
	gangName, availableCount := p.Labels[LabelGangName], p.Labels[LabelGangAvailableCount]
	if gangName == "" || availableCount == "" {
		return framework.NewStatus(framework.Success, "not gang-pod"), 0
	}

	_, ok := g.rejectedGangs.Load(gangName)
	if ok {
		return framework.NewStatus(framework.UnschedulableAndUnresolvable, "gang "+gangName+" has been rejected"), 0
	}

	runningPods, _, err := g.getRunningAndPendingPodsInGang(gangName)
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

	permittedButNotRunningPods := 0
	permittedPods, ok := g.permittedGangPods.Load(gangName)
	if ok {
		g.mu.Lock()
		// add permitted but not running pods
		permittedPodsMap := permittedPods.(map[string]bool)
		runningPodsMap := make(map[string]bool)
		for _, pod := range runningPods {
			runningPodsMap[pod.Namespace+"/"+pod.Name] = true
		}
		for pod := range permittedPodsMap {
			if !runningPodsMap[pod] {
				permittedButNotRunningPods++
			}
		}
		g.mu.Unlock()
	}

	if len(waitingPods)+permittedButNotRunningPods+len(runningPods)+1 < availableCnt {
		klog.Infof("Permit found not enough gang %v's pod %v, %v waiting + %v permitted + %v running + 1 < "+
			"%v available needed", gangName, p.Name, len(waitingPods), permittedButNotRunningPods, len(runningPods), availableCnt)
		return framework.NewStatus(framework.Wait, fmt.Sprintf("waiting for pod count to reach availableCnt %v: %v waiting, %v running",
			availableCnt, len(waitingPods), len(runningPods))), time.Minute
	}

	// permit the entire gang
	klog.Infof("Permit allow gang %v's pod %v, allowing the entire gang for %v waiting + %v permitted "+
		"+ %v running + 1 >= %v available needed",
		gangName, p.Name, len(waitingPods), permittedButNotRunningPods, len(runningPods), availableCnt)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.handle.IterateOverWaitingPods(func(pod framework.WaitingPod) {
		if pod.GetPod().Labels[LabelGangName] == gangName {
			pod.Allow(g.Name())
			g.setPermittedInPodMap(gangName, pod.GetPod())
		}
	})
	g.setPermittedInPodMap(gangName, p)
	return framework.NewStatus(framework.Success, "gang availableCnt meet: "+fmt.Sprintf("%d",
		len(waitingPods)+permittedButNotRunningPods+len(runningPods)+1)), 0
}

func (g *GangScheduler) Name() string {
	return "unicore-gang-scheduler"
}

func (g *GangScheduler) setPermittedInPodMap(gangName string, pod *v1.Pod) {
	permittedPodsMap := make(map[string]bool)
	permittedPods, ok := g.permittedGangPods.Load(gangName)
	if ok {
		permittedPodsMap = permittedPods.(map[string]bool)
	}
	permittedPodsMap[pod.Namespace+"/"+pod.Name] = true
	g.permittedGangPods.Store(gangName, permittedPodsMap)
}

func (g *GangScheduler) getWaitingPodsInGang(gangName string) []*v1.Pod {
	ret := make([]*v1.Pod, 0)
	g.handle.IterateOverWaitingPods(func(pod framework.WaitingPod) {
		if pod.GetPod().Labels[LabelGangName] == gangName {
			ret = append(ret, pod.GetPod())
		}
	})
	return ret
}

func (g *GangScheduler) getRunningAndPendingPodsInGang(gangName string) ([]*v1.Pod, []*v1.Pod, error) {
	pods, err := g.podLister.List(labels.SelectorFromSet(map[string]string{LabelGangName: gangName}))
	if err != nil {
		return nil, nil, err
	}
	running := make([]*v1.Pod, 0)
	pending := make([]*v1.Pod, 0)
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodRunning {
			running = append(running, pod)
		} else if pod.Status.Phase == v1.PodPending {
			pending = append(pending, pod)
		}
	}
	return running, pending, nil
}
