package app

import (
	"context"
	"fmt"
	unicore "github.com/mcyouyou/unicore/api/deployer/v1"
	"github.com/mcyouyou/unicore/api/deployer/v1/inplace"
	"github.com/mcyouyou/unicore/internal/controller/requeue_duration"
	"github.com/mcyouyou/unicore/pkg/generated/clientset/versioned"
	lister "github.com/mcyouyou/unicore/pkg/generated/listers/deployer/v1"
	apps "k8s.io/api/apps/v1"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/controller/history"
	"k8s.io/utils/ptr"
	"math"
	"sort"
	"time"
)

var (
	controllerKind = unicore.GroupVersion.WithKind("App")
)

type StateController struct {
	podController *PodController
	recorder      record.EventRecorder
	history       history.Interface
	// client for app
	client    versioned.Interface
	appLister lister.AppLister
}

// NewStateController new a stateController to manage revision and state
func NewStateController(podController *PodController, recorder record.EventRecorder, history history.Interface,
	client versioned.Interface, appLister lister.AppLister) *StateController {
	return &StateController{
		podController: podController,
		recorder:      recorder,
		history:       history,
		client:        client,
		appLister:     appLister,
	}
}

func (c *StateController) UpdateApp(ctx context.Context, app *unicore.App, pods []*v1.Pod) error {
	if app == nil {
		return fmt.Errorf("app is nil")
	}
	// do a copy since it may be modified
	app = app.DeepCopy()
	// list and sort revisions from apiserver
	selector, err := metav1.LabelSelectorAsSelector(app.Spec.Selector)
	if err != nil {
		return err
	}
	revisions, err := c.history.ListControllerRevisions(app, selector)
	if err != nil {
		return err
	}
	history.SortControllerRevisions(revisions)
	collisionCount := int32(0)
	if app.Status.CollisionCount != nil {
		collisionCount = *app.Status.CollisionCount
	}

	// create new revision from to-update app(marshaling)
	patch, err := getAppPatch(app)
	if err != nil {
		return err
	}
	nextRevisionId := int64(1)
	if len(revisions) > 0 {
		nextRevisionId = revisions[len(revisions)-1].Revision + 1
	}
	cr, err := history.NewControllerRevision(app, controllerKind, app.Spec.Template.Labels,
		runtime.RawExtension{Raw: patch}, nextRevisionId, &collisionCount)
	if err != nil {
		return err
	}
	if cr.ObjectMeta.Annotations == nil {
		cr.ObjectMeta.Annotations = make(map[string]string)
	}
	for k, v := range app.Annotations {
		cr.ObjectMeta.Annotations[k] = v
	}

	// check if equivalent revision exists, then decide whether to issue creating or updating(rolling back) to apiserver
	equalRevisions := history.FindEqualRevisions(revisions, cr)
	equalCnt := len(equalRevisions)
	if equalCnt != 0 {
		if history.EqualRevision(revisions[len(revisions)-1], equalRevisions[equalCnt-1]) {
			// newest revision is up to date, nothing changed
			cr = revisions[len(revisions)-1]
		} else {
			// same revision found in history, rollback to it by updating the old revision's RevisionID
			cr, err = c.history.UpdateControllerRevision(equalRevisions[equalCnt-1], cr.Revision)
			if err != nil {
				return err
			}
		}
	} else {
		// issue a creat req to apiserver, adding the collisionCount if hash collision
		cr, err = c.history.CreateControllerRevision(app, cr, &collisionCount)
		if err != nil {
			return err
		}
	}

	// try finding the corresponding revision to the app.status
	updateRevision := cr
	var currentRevision *apps.ControllerRevision
	for i := range revisions {
		if revisions[i].Name == app.Status.CurrentRevision {
			currentRevision = revisions[i]
			break
		}
	}
	if currentRevision == nil {
		currentRevision = updateRevision
	}

	currentStatus, getStatusErr := c.applyUpdate(ctx, app, currentRevision, updateRevision, collisionCount, pods, revisions)
	if getStatusErr != nil && currentStatus == nil {
		return getStatusErr
	}

	updateStatusErr := c.updateAppStatus(ctx, app, currentStatus)
	if updateStatusErr == nil {
		klog.V(4).InfoS("update app status success", "app", klog.KObj(app),
			"replicas", currentStatus.Replicas,
			"readyReplicas", currentStatus.ReadyReplicas,
			"currentReplicas", currentStatus.CurrentReplicas,
			"updatedReplicas", currentStatus.UpdatedReplicas)
	}

	err = nil
	if getStatusErr != nil && updateStatusErr != nil {
		klog.ErrorS(updateStatusErr, "can not update status", "app", klog.KObj(app))
		err = getStatusErr
	} else if getStatusErr != nil {
		err = getStatusErr
	} else if updateStatusErr != nil {
		err = updateStatusErr
	} else {
		klog.V(4).InfoS("update status success", "app", klog.KObj(app),
			"currentRevision", currentStatus.CurrentRevision,
			"updateRevision", currentStatus.UpdateRevision)
	}
	truncateErr := c.truncateHistory(app, pods, revisions, currentRevision, updateRevision)
	if err != nil {
		if truncateErr != nil {
			klog.ErrorS(truncateErr, "can not truncate", "app", klog.KObj(app), "err", truncateErr.Error())
		}
		return err
	}
	return truncateErr
}

// core procedure of updating the status and pods
func (c *StateController) applyUpdate(ctx context.Context,
	app *unicore.App,
	currentRevision *apps.ControllerRevision,
	updateRevision *apps.ControllerRevision,
	collisionCount int32,
	pods []*v1.Pod,
	revisions []*apps.ControllerRevision) (*unicore.AppStatus, error) {
	selector, err := metav1.LabelSelectorAsSelector(app.Spec.Selector)
	if err != nil {
		return app.Status.DeepCopy(), err
	}

	currentApp, err := ApplyRevision(app, currentRevision)
	if err != nil {
		return app.Status.DeepCopy(), err
	}
	updateApp, err := ApplyRevision(app, updateRevision)
	if err != nil {
		return app.Status.DeepCopy(), err
	}

	status := unicore.AppStatus{}
	status.CurrentRevision = currentRevision.Name
	status.UpdateRevision = updateRevision.Name
	status.ObservedGeneration = app.Generation
	status.CollisionCount = ptr.To[int32](collisionCount)
	status.LabelSelector = selector.String()
	minReadySeconds := getMinReadySeconds(app)
	// update App.Status
	updateStatus(&status, minReadySeconds, currentRevision, updateRevision, pods)

	// set default rollingUpdateStrategy if needed
	if app.Spec.UpdateStrategy.Type == apps.RollingUpdateStatefulSetStrategyType {
		updateNeeded, newRollingUpdate := setDefaultRollingUpdateStrategy(app)
		if updateNeeded {
			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				clone := app.DeepCopy()
				clone.Spec.UpdateStrategy.RollingUpdate = newRollingUpdate
				app, err = c.client.UnicoreV1().Apps(clone.Namespace).Update(ctx, clone, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				klog.Infof("added default rollingUpdate strategy to app %v", app.Name)
				return nil
			})
			if err != nil {
				klog.Infof("failed adding default rollingUpdate strategy to app %v: %v", app.Name, err)
			}
			return &status, nil
		}
	}

	// the new replica list to maintain
	replicaCnt := int(*app.Spec.Replicas)
	replicas := make([]*v1.Pod, replicaCnt)
	firstUnhealthyOrdinal := math.MaxInt32
	var firstUnhealthyPod *v1.Pod
	invalidPods := make([]*v1.Pod, 0)

	burstable := app.Spec.PodManagementPolicy == apps.ParallelPodManagement
	// filter invalid ordinal pod, fill the pod to replica list with their index
	for i := range pods {
		if _, ord := GetPodAppNameAndOrdinal(pods[i]); ord < replicaCnt {
			replicas[ord] = pods[i]
		} else if ord >= 0 {
			invalidPods = append(invalidPods, pods[i])
		}
	}

	// create extra pod to fill the empty slot of replica list
	// this is where the newer version pod is created
	for i := 0; i < replicaCnt; i++ {
		if replicas[i] == nil {
			replicas[i] = newVersionedPodForApp(currentApp, updateApp, currentRevision.Name, updateRevision.Name,
				i, replicas)
		}
	}
	// sort invalid pods by their ordinals, descending
	sort.Sort(descendingOrdinal(invalidPods))

	unhealthy := 0
	for i := range replicas {
		if replicas[i] == nil {
			continue
		}
		if !getPodReady(replicas[i]) || replicas[i].DeletionTimestamp != nil {
			unhealthy++
			if _, ord := GetPodAppNameAndOrdinal(replicas[i]); ord < firstUnhealthyOrdinal {
				firstUnhealthyOrdinal = ord
				firstUnhealthyPod = replicas[i]
			}
		}
	}

	for i := len(invalidPods) - 1; i >= 0; i-- {
		if !getPodReady(invalidPods[i]) || invalidPods[i].DeletionTimestamp != nil {
			unhealthy++
			if _, ord := GetPodAppNameAndOrdinal(invalidPods[i]); ord < firstUnhealthyOrdinal {
				firstUnhealthyOrdinal = ord
				firstUnhealthyPod = invalidPods[i]
			}
		}
	}

	if unhealthy > 0 {
		klog.V(4).InfoS("App has unhealthy pods", "app", klog.KObj(app), "unhealthyReplicas", unhealthy, "pod", klog.KObj(firstUnhealthyPod))
	}

	// now we check every pod that should be valid, delete it if failed, create it if not created, and wait until
	// 	all pods are there and match their identities.
	// note that we use pods.Update() to match pod's identity with the app, only editing its labels and annotations,
	// 	not its revision.
	allAvailable := true
	logger := klog.FromContext(ctx)
	var runErr error
	for i := range replicas {
		if replicas[i] == nil {
			continue
		}
		// pods in these two phase should be restarted
		if replicas[i].Status.Phase == v1.PodFailed || replicas[i].Status.Phase == v1.PodSucceeded {
			allAvailable = false
			if replicas[i].DeletionTimestamp == nil {
				if err := c.podController.DeleteStatefulPod(ctx, app, replicas[i]); err != nil {
					c.recorder.Eventf(app, v1.EventTypeWarning, "FailedDelete", "failed to delete pod %s: %v", replicas[i].Name, err)
					runErr = err
				}
			}
			break
		}

		// if pod not created, create one
		if replicas[i].Status.Phase == "" {
			allAvailable = false
			if err := c.podController.CreateStatefulPod(ctx, app, replicas[i]); err != nil {
				condition := apps.StatefulSetCondition{
					Type:    unicore.ConditionFailCreatePod,
					Status:  v1.ConditionTrue,
					Message: fmt.Sprintf("failed to create pod %s: %v", replicas[i].Name, err),
				}
				setAppCondition(&status, condition)
				runErr = err
			}
			// if burstable not allowed, break to make a monotonic creation
			if !burstable {
				break
			} else {
				continue
			}
		}

		if replicas[i].Status.Phase == v1.PodPending {
			allAvailable = false
			logger.V(4).Info("App is creating pvc for pending pod", "app", klog.KObj(app), klog.KObj(replicas[i]))
			if err := c.podController.createPVC(app, replicas[i]); err != nil {
				runErr = err
				break
			}
		}

		// no bursting: for terminating pod: wait until graceful exit
		if replicas[i].DeletionTimestamp != nil && !burstable {
			logger.V(4).Info("App is waiting for pod to terminate", "app", klog.KObj(app), "pod", klog.KObj(replicas[i]))
			allAvailable = false
			break
		}
		if replicas[i].DeletionTimestamp != nil && burstable {
			logger.V(4).Info("App pod is terminating, skip this loop", "app", klog.KObj(app), "pod", klog.KObj(replicas[i]))
			break
		}

		// TODO: Update InPlaceUpdateReady condition for pod

		isAvailable, checkInterval := getPodAvailableAndNextCheckInterval(replicas[i], minReadySeconds)
		if !burstable {
			// not burstable and a pod is unavailable, preceding procedures can't be done
			if !isAvailable {
				allAvailable = false
				if checkInterval > 0 {
					// check it next reconcile
					requeue_duration.Push(GetAppKey(app), checkInterval)
					logger.V(4).Info("waiting for pod to be available after ready for minReadySeconds",
						"app", klog.KObj(app), "waitTime", checkInterval, "pod", klog.KObj(replicas[i]),
						"minReadySeconds", minReadySeconds)
				} else {
					logger.V(4).Info("waiting for pod to be available", "app", klog.KObj(app), "pod", klog.KObj(replicas[i]))
				}
				break
			}
		} else if !isAvailable {
			// burstable but unavailable
			klog.Info("app pod is unavailable, skip this loop", "app", klog.KObj(app), "pod", klog.KObj(replicas[i]))
			if checkInterval > 0 {
				// check it next reconcile
				requeue_duration.Push(GetAppKey(app), checkInterval)
			}
			break
		}

		// if pod identity mismatch, update its identity
		if !matchAppAndPod(app, replicas[i]) || !matchAppPVC(app, replicas[i]) {
			// avoid changing shared cache
			replica := replicas[i].DeepCopy()
			// update pod's identity to match the app
			if err := c.podController.UpdateStatefulPod(ctx, app, replica); err != nil {
				condition := apps.StatefulSetCondition{
					Type:    unicore.ConditionFailUpdatePod,
					Status:  v1.ConditionTrue,
					Message: fmt.Sprintf("failed to update pod %s: %v", replicas[i].Name, err),
				}
				setAppCondition(&status, condition)
				runErr = err
				break
			}
		}
	}

	if runErr != nil || !allAvailable {
		updateStatus(&status, minReadySeconds, currentRevision, updateRevision, replicas, invalidPods)
		return &status, runErr
	}

	// at this point, if not burstable, all pods of spec.Replicas are available.
	// then we can make deletion of invalid pods in a decreasing order, if all predecessors are ready
	shouldExit := false
	for i := range invalidPods {
		pod := invalidPods[i]
		if pod.DeletionTimestamp != nil {
			if !burstable {
				// if not burstable, exit and wait until pod terminated
				shouldExit = true
				klog.Info("waiting for pod to terminate before scaling down", "app",
					klog.KObj(app), "pod", klog.KObj(pod))
				break
			}
		}
		// not burstable: if it's not the first unhealthy pod, exit until predecessors are ready
		if !burstable && !getPodReady(pod) && pod != firstUnhealthyPod {
			logger.V(4).Info("waiting for preceding pod to be ready before scaling down", "app",
				klog.KObj(app), "pod", klog.KObj(pod), "preceding-pod", klog.KObj(firstUnhealthyPod))
			shouldExit = true
			break
		}

		klog.Info("pod is terminating for scale down app ", klog.KObj(app), " 's pod ", klog.KObj(pod))
		runErr = c.podController.DeleteStatefulPod(ctx, app, invalidPods[i])
		if runErr != nil {
			c.recorder.Eventf(app, v1.EventTypeWarning, "FailedDelete", "failed to delete pod %s: %v", invalidPods[i].Name, runErr)
			shouldExit = true
			break
		}

		if !burstable {
			shouldExit = true
			break
		}
	}

	updateStatus(&status, minReadySeconds, currentRevision, updateRevision, replicas, invalidPods)
	if shouldExit || runErr != nil {
		return &status, runErr
	}

	// for onDelete strategy, pods will only be updated when manually deleted
	if app.Spec.UpdateStrategy.Type == apps.OnDeleteStatefulSetStrategyType {
		return &status, nil
	}

	if app.DeletionTimestamp != nil {
		// app is being deleted
		return &status, nil
	}

	// for rollingUpdate strategy, we terminate the pod with the largest ordinal that does not match the updateRevision
	updateMin := 0
	if app.Spec.UpdateStrategy.RollingUpdate.Paused {
		klog.InfoS("rolling update paused, skipping", "app", app.Name)
		return &status, nil
	}

	// update can only be done for pod [partition:]
	updateMin = int(*app.Spec.UpdateStrategy.RollingUpdate.Partition)
	for target := len(replicas) - 1; target >= updateMin; target-- {
		if getPodRevision(replicas[target]) != updateRevision.Name && replicas[target].DeletionTimestamp == nil {
			if app.Spec.UpdateStrategy.RollingUpdate != nil &&
				app.Spec.UpdateStrategy.RollingUpdate.PodUpdatePolicy == unicore.InPlaceIfPossiblePodUpdateStrategyType {
				// try in-place update
				err, waitSeconds := c.InPlaceUpdatePod(app, replicas[target], updateRevision, revisions)
				if err == nil {
					if waitSeconds > 0 {
						requeue_duration.Push(GetAppKey(app), time.Duration(waitSeconds)*time.Second)
						klog.InfoS("started in-place update, waiting for grace seconds", "app",
							klog.KObj(app), "pod", klog.KObj(replicas[target]), "graceSeconds", waitSeconds)
					} else {
						klog.InfoS("started in-place update", "app",
							klog.KObj(app), "pod", klog.KObj(replicas[target]))
					}
					return &status, err
				}
				klog.InfoS("can't use in-place update, falling back to ReCreate", "err",
					err, "app", klog.KObj(app), "pod", klog.KObj(replicas[target]))
			}

			// normal ReCreate update
			klog.InfoS("terminating pod for update", "app", klog.KObj(app), "pod", klog.KObj(replicas[target]))
			if err := c.podController.DeleteStatefulPod(ctx, app, replicas[target]); err != nil {
				if !errors.IsNotFound(err) {
					return &status, err
				}
			}
			status.CurrentReplicas--
			return &status, err
		}

		// check whether in-place update is done, if in-place update is set
		if replicas[target].Annotations[inplace.StatusKey] != "" {
			inPlaceCondExist := false
			for _, condition := range replicas[target].Status.Conditions {
				if condition.Type == InPlaceUpdateReady && condition.Status != v1.ConditionTrue {
					inPlaceCondExist = true
					break
				}
			}
			if inPlaceCondExist {
				done, err := IsInPlaceUpdateDone(replicas[target])
				if err != nil {
					klog.InfoS("get in-place update status err", "err", err, "app",
						klog.KObj(app), "pod", klog.KObj(replicas[target]))
					return &status, err
				}
				if !done {
					klog.InfoS("waiting for in-place update container to rebuild", "app",
						klog.KObj(app), "pod", klog.KObj(replicas[target]))
					return &status, err
				}

				klog.InfoS("in-place update for pod finished, removing condition", "app",
					klog.KObj(app), "pod", klog.KObj(replicas[target]))

				err = c.CleanUpInPlaceUpdate(replicas[target])
				if err != nil {
					return &status, err
				}
				continue
			}
		}

		// wait for unhealthy pods to update
		if !(getPodReady(replicas[target]) || replicas[target].DeletionTimestamp != nil) {
			klog.InfoS("waiting for pod to update", "app", klog.KObj(app), "pod", klog.KObj(replicas[target]))
			return &status, nil
		}
	}

	return &status, nil
}

func (c *StateController) updateAppStatus(ctx context.Context, app *unicore.App, status *unicore.AppStatus) error {
	// if rollingUpdate done, update its status
	completeRollingUpdate(app, status)

	// status consistent, no need to update
	if statusConsistent(app, status) {
		return nil
	}

	// it's going to be modified, copy one
	app = app.DeepCopy()
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		app.Status = *status
		_, err := c.client.UnicoreV1().Apps(app.Namespace).UpdateStatus(ctx, app, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}
		if updated, err := c.appLister.Apps(app.Namespace).Get(app.Name); err == nil {
			app = updated.DeepCopy()
		} else {
			utilruntime.HandleError(fmt.Errorf("err getting updated app %s/%s from lister: %v", app.Namespace, app.Name, err))
		}
		return err
	})
}

// truncate revisions that are not live(no pod use it, and isn't currentRevision/updateRevision) to maintain the revisionLimit
func (c *StateController) truncateHistory(app *unicore.App, pods []*v1.Pod, revisions []*apps.ControllerRevision,
	current, update *apps.ControllerRevision) error {
	history := make([]*apps.ControllerRevision, 0, len(revisions))
	live := make(map[string]bool)
	if current != nil {
		live[current.Name] = true
	}
	if update != nil {
		live[update.Name] = true
	}
	for i := range pods {
		live[getPodRevision(pods[i])] = true
	}
	for i := range revisions {
		if !live[revisions[i].Name] {
			history = append(history, revisions[i])
		}
	}

	if app.Spec.RevisionHistoryLimit == nil {
		defaultRevisionHistoryLimit := int32(10)
		app.Spec.RevisionHistoryLimit = &defaultRevisionHistoryLimit
	}
	limit := int(*app.Spec.RevisionHistoryLimit)
	if len(history) > limit {
		toDelete := history[:len(history)-limit]
		for i := range toDelete {
			if err := c.history.DeleteControllerRevision(toDelete[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *StateController) ListRevisions(app *unicore.App) ([]*apps.ControllerRevision, error) {
	selector, err := metav1.LabelSelectorAsSelector(app.Spec.Selector)
	if err != nil {
		return nil, err
	}
	return c.history.ListControllerRevisions(app, selector)
}

// AdoptOrphanRevisions adding ControllerRef to metadata to adopt the revision
func (c *StateController) AdoptOrphanRevisions(app *unicore.App, revisions []*apps.ControllerRevision) error {
	for i := range revisions {
		adopted, err := c.history.AdoptControllerRevision(app, controllerKind, revisions[i])
		if err != nil {
			return err
		}
		revisions[i] = adopted
	}
	return nil
}

// check if ObservedGeneration is less or equal to the app's Generation and all fields of status match the app
func statusConsistent(app *unicore.App, status *unicore.AppStatus) bool {
	if status.ObservedGeneration > app.Status.ObservedGeneration || status.Replicas != app.Status.Replicas ||
		status.CurrentReplicas != app.Status.CurrentReplicas || status.ReadyReplicas != app.Status.ReadyReplicas ||
		status.AvailableReplicas != app.Status.AvailableReplicas || status.UpdatedReplicas != app.Status.UpdatedReplicas ||
		status.CurrentRevision != app.Status.CurrentRevision || status.UpdateRevision != app.Status.UpdateRevision ||
		status.LabelSelector != app.Status.LabelSelector {
		return false
	}

	vcIndex := make(map[string]int)
	for i, v := range status.VolumeClaims {
		vcIndex[v.VolumeClaimName] = i
	}
	for _, v := range app.Status.VolumeClaims {
		if idx, ok := vcIndex[v.VolumeClaimName]; !ok {
			return false
		} else if status.VolumeClaims[idx].CompatibleReplicas != v.CompatibleReplicas ||
			status.VolumeClaims[idx].CompatibleReadyReplicas != v.CompatibleReadyReplicas {
			return false
		}
	}
	return true
}

// update status to finish a RollingUpdate if it's done
func completeRollingUpdate(app *unicore.App, status *unicore.AppStatus) {
	if app.Spec.UpdateStrategy.Type == apps.RollingUpdateStatefulSetStrategyType && status.UpdatedReplicas == status.Replicas &&
		status.ReadyReplicas == status.Replicas {
		status.CurrentReplicas = status.UpdatedReplicas
		status.CurrentRevision = status.UpdateRevision
	}
}

// decide and create a pod for an app either from currentRevision or updateRevision
func newVersionedPodForApp(currentApp, updateApp *unicore.App, currentRevision, updateRevision string, ordinal int,
	replicas []*v1.Pod) *v1.Pod {
	if isCurrentRevisionExpected(currentApp, updateRevision, ordinal, replicas) {
		pod := newAppPod(currentApp, ordinal)
		if pod.Labels == nil {
			pod.Labels = make(map[string]string)
		}
		pod.Labels[apps.StatefulSetRevisionLabel] = currentRevision
		return pod
	}
	pod := newAppPod(updateApp, ordinal)
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[apps.StatefulSetRevisionLabel] = updateRevision
	return pod
}

// check if the given ordinal pod is expected to be at currentRevision instead of updateRevision
func isCurrentRevisionExpected(app *unicore.App, updateRevision string, ordinal int, replicas []*v1.Pod) bool {
	// use no rolling update and revisions, from spec data to create pods instead
	if app.Spec.UpdateStrategy.Type != apps.RollingUpdateStatefulSetStrategyType {
		return false
	}
	if app.Spec.UpdateStrategy.RollingUpdate == nil {
		{
			return ordinal < int(app.Status.CurrentReplicas)
		}
	}
	// use ordered update, pods ordinals in [:RollingUpdate.Partition) are expected to be at CurrentRevision
	if !app.Spec.UpdateStrategy.RollingUpdate.UnorderedUpdate {
		return ordinal < int(*app.Spec.UpdateStrategy.RollingUpdate.Partition)
	}
	// if unordered strategy is set, Partition is the expected amount of pods at CurrentRevision
	var currentRevisionCnt int
	for i, pod := range replicas {
		if pod == nil || i == ordinal {
			continue
		}
		if pod.GetLabels()[apps.ControllerRevisionHashLabelKey] != updateRevision {
			currentRevisionCnt++
		}
	}
	return currentRevisionCnt < int(*app.Spec.UpdateStrategy.RollingUpdate.Partition)
}

func getAppConditionFromType(status *unicore.AppStatus, condType apps.StatefulSetConditionType) *apps.StatefulSetCondition {
	for i := range status.Conditions {
		c := status.Conditions[i]
		if c.Type == condType {
			return &c
		}
	}
	return nil
}

// update status.Conditions with given condition
func setAppCondition(status *unicore.AppStatus, condition apps.StatefulSetCondition) {
	currentCondition := getAppConditionFromType(status, condition.Type)
	if currentCondition != nil && currentCondition.Status == condition.Status && currentCondition.Reason == condition.Reason {
		return
	}
	if currentCondition != nil && currentCondition.Status == condition.Status {
		condition.LastTransitionTime = currentCondition.LastTransitionTime
	}

	// filter out all items with the same condition type
	newConditions := make([]apps.StatefulSetCondition, 0)
	for _, c := range status.Conditions {
		if c.Type != condition.Type {
			newConditions = append(newConditions, c)
		}
	}
	status.Conditions = append(newConditions, condition)
}

func setDefaultRollingUpdateStrategy(app *unicore.App) (updateNeeded bool, newRollingUpdate *unicore.RollingUpdateAppStrategy) {
	zero := int32(0)
	newRollingUpdate = &unicore.RollingUpdateAppStrategy{
		Partition:             &zero,
		MaxUnavailable:        &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
		PodUpdatePolicy:       unicore.RecreatePodUpdateStrategyType,
		Paused:                false,
		UnorderedUpdate:       false,
		InPlaceUpdateStrategy: nil,
		MinReadySeconds:       &zero,
	}
	updateNeeded = false
	if app.Spec.UpdateStrategy.RollingUpdate == nil {
		updateNeeded = true
	} else {
		if app.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
			updateNeeded = true
		} else {
			newRollingUpdate.Partition = app.Spec.UpdateStrategy.RollingUpdate.Partition
		}

		if app.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable == nil {
			updateNeeded = true
		} else {
			newRollingUpdate.MaxUnavailable = app.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable
		}

		if app.Spec.UpdateStrategy.RollingUpdate.PodUpdatePolicy == "" {
			updateNeeded = true
		} else {
			newRollingUpdate.PodUpdatePolicy = app.Spec.UpdateStrategy.RollingUpdate.PodUpdatePolicy
		}

		if app.Spec.UpdateStrategy.RollingUpdate.MinReadySeconds == nil {
			updateNeeded = true
		} else {
			newRollingUpdate.MinReadySeconds = app.Spec.UpdateStrategy.RollingUpdate.MinReadySeconds
		}

		if app.Spec.UpdateStrategy.RollingUpdate.InPlaceUpdateStrategy != nil {
			newRollingUpdate.InPlaceUpdateStrategy = app.Spec.UpdateStrategy.RollingUpdate.InPlaceUpdateStrategy
		}
	}
	return updateNeeded, newRollingUpdate
}
