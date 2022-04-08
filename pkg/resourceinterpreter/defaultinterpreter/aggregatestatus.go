package defaultinterpreter

import (
	"encoding/json"
	"reflect"

	snapv1 "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	appsv1 "k8s.io/api/apps/v1"
	asv1 "k8s.io/api/autoscaling/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"

	workv1alpha2 "github.com/karmada-io/karmada/pkg/apis/work/v1alpha2"
	"github.com/karmada-io/karmada/pkg/util"
	"github.com/karmada-io/karmada/pkg/util/helper"
)

type aggregateStatusInterpreter func(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error)

func getAllDefaultAggregateStatusInterpreter() map[schema.GroupVersionKind]aggregateStatusInterpreter {
	s := make(map[schema.GroupVersionKind]aggregateStatusInterpreter)
	s[appsv1.SchemeGroupVersion.WithKind(util.DeploymentKind)] = aggregateDeploymentStatus
	s[corev1.SchemeGroupVersion.WithKind(util.ServiceKind)] = aggregateServiceStatus
	s[extensionsv1beta1.SchemeGroupVersion.WithKind(util.IngressKind)] = aggregateIngressStatus
	s[batchv1.SchemeGroupVersion.WithKind(util.JobKind)] = aggregateJobStatus
	s[appsv1.SchemeGroupVersion.WithKind(util.DaemonSetKind)] = aggregateDaemonSetStatus

	// 这些资源只在proxy场景下生效，分发到多个集群的话不生效
	s[corev1.SchemeGroupVersion.WithKind(util.PersistentVolumeClaimKind)] = aggregatePVCStatus
	s[snapv1.SchemeGroupVersion.WithKind(util.VolumeSnapshotKind)] = aggregateVolumeSnapshotStatus
	s[asv1.SchemeGroupVersion.WithKind(util.HorizontalPodAutoscalerKind)] = aggregateHPAStatus
	return s
}

func aggregateDeploymentStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	deploy, err := helper.ConvertToDeployment(object)
	if err != nil {
		return nil, err
	}

	oldStatus := &deploy.Status
	newStatus := &appsv1.DeploymentStatus{}
	for _, item := range aggregatedStatusItems {
		if item.Status == nil {
			continue
		}
		temp := &appsv1.DeploymentStatus{}
		if err = json.Unmarshal(item.Status.Raw, temp); err != nil {
			return nil, err
		}
		klog.V(3).Infof("Grab deployment(%s/%s) status from cluster(%s), replicas: %d, ready: %d, updated: %d, available: %d, unavailable: %d",
			deploy.Namespace, deploy.Name, item.ClusterName, temp.Replicas, temp.ReadyReplicas, temp.UpdatedReplicas, temp.AvailableReplicas, temp.UnavailableReplicas)
		newStatus.ObservedGeneration = deploy.Generation
		newStatus.Replicas += temp.Replicas
		newStatus.ReadyReplicas += temp.ReadyReplicas
		newStatus.UpdatedReplicas += temp.UpdatedReplicas
		newStatus.AvailableReplicas += temp.AvailableReplicas
		newStatus.UnavailableReplicas += temp.UnavailableReplicas
		if len(aggregatedStatusItems) == 1 {
			newStatus.Conditions = temp.Conditions
		}
	}

	if oldStatus.ObservedGeneration == newStatus.ObservedGeneration &&
		oldStatus.Replicas == newStatus.Replicas &&
		oldStatus.ReadyReplicas == newStatus.ReadyReplicas &&
		oldStatus.UpdatedReplicas == newStatus.UpdatedReplicas &&
		oldStatus.AvailableReplicas == newStatus.AvailableReplicas &&
		oldStatus.UnavailableReplicas == newStatus.UnavailableReplicas &&
		reflect.DeepEqual(oldStatus.Conditions, newStatus.Conditions) {
		klog.V(3).Infof("ignore update deployment(%s/%s) status as up to date", deploy.Namespace, deploy.Name)
		return object, nil
	}

	oldStatus.ObservedGeneration = newStatus.ObservedGeneration
	oldStatus.Replicas = newStatus.Replicas
	oldStatus.ReadyReplicas = newStatus.ReadyReplicas
	oldStatus.UpdatedReplicas = newStatus.UpdatedReplicas
	oldStatus.AvailableReplicas = newStatus.AvailableReplicas
	oldStatus.UnavailableReplicas = newStatus.UnavailableReplicas
	oldStatus.Conditions = newStatus.Conditions

	return helper.ToUnstructured(deploy)
}

func aggregateServiceStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	service, err := helper.ConvertToService(object)
	if err != nil {
		return nil, err
	}

	if service.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return object, nil
	}

	// If service type is of type LoadBalancer, collect the status.loadBalancer.ingress
	newStatus := &corev1.ServiceStatus{}
	for _, item := range aggregatedStatusItems {
		if item.Status == nil {
			continue
		}
		temp := &corev1.ServiceStatus{}
		if err := json.Unmarshal(item.Status.Raw, temp); err != nil {
			klog.Errorf("Failed to unmarshal status of service(%s/%s): %v", service.Namespace, service.Name, err)
			return nil, err
		}
		klog.V(3).Infof("Grab service(%s/%s) status from cluster(%s), loadBalancer status: %v",
			service.Namespace, service.Name, item.ClusterName, temp.LoadBalancer)

		// Set cluster name as Hostname by default to indicate the status is collected from which member cluster.
		for i := range temp.LoadBalancer.Ingress {
			if temp.LoadBalancer.Ingress[i].Hostname == "" {
				temp.LoadBalancer.Ingress[i].Hostname = item.ClusterName
			}
		}

		newStatus.LoadBalancer.Ingress = append(newStatus.LoadBalancer.Ingress, temp.LoadBalancer.Ingress...)
	}

	if reflect.DeepEqual(service.Status, *newStatus) {
		klog.V(3).Infof("ignore update service(%s/%s) status as up to date", service.Namespace, service.Name)
		return object, nil
	}

	service.Status = *newStatus
	return helper.ToUnstructured(service)
}

func aggregateIngressStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	ingress, err := helper.ConvertToIngress(object)
	if err != nil {
		return nil, err
	}

	newStatus := &extensionsv1beta1.IngressStatus{}
	for _, item := range aggregatedStatusItems {
		if item.Status == nil {
			continue
		}
		temp := &extensionsv1beta1.IngressStatus{}
		if err := json.Unmarshal(item.Status.Raw, temp); err != nil {
			klog.Errorf("Failed to unmarshal status ingress(%s/%s): %v", ingress.Namespace, ingress.Name, err)
			return nil, err
		}
		klog.V(3).Infof("Grab ingress(%s/%s) status from cluster(%s), loadBalancer status: %v",
			ingress.Namespace, ingress.Name, item.ClusterName, temp.LoadBalancer)

		// Set cluster name as Hostname by default to indicate the status is collected from which member cluster.
		for i := range temp.LoadBalancer.Ingress {
			if temp.LoadBalancer.Ingress[i].Hostname == "" {
				temp.LoadBalancer.Ingress[i].Hostname = item.ClusterName
			}
		}

		newStatus.LoadBalancer.Ingress = append(newStatus.LoadBalancer.Ingress, temp.LoadBalancer.Ingress...)
	}

	if reflect.DeepEqual(ingress.Status, *newStatus) {
		klog.V(3).Infof("ignore update ingress(%s/%s) status as up to date", ingress.Namespace, ingress.Name)
		return object, nil
	}

	ingress.Status = *newStatus
	return helper.ToUnstructured(ingress)
}

func aggregateJobStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	job, err := helper.ConvertToJob(object)
	if err != nil {
		return nil, err
	}

	newStatus, err := helper.ParsingJobStatus(job, aggregatedStatusItems)
	if err != nil {
		return nil, err
	}

	if reflect.DeepEqual(job.Status, *newStatus) {
		klog.V(3).Infof("ignore update job(%s/%s) status as up to date", job.Namespace, job.Name)
		return object, nil
	}

	job.Status = *newStatus
	return helper.ToUnstructured(job)
}

func aggregateDaemonSetStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	ds, err := helper.ConvertToDaemonSet(object)
	if err != nil {
		return nil, err
	}

	newStatus := &appsv1.DaemonSetStatus{}
	for _, item := range aggregatedStatusItems {
		if item.Status == nil {
			continue
		}

		temp := &appsv1.DaemonSetStatus{}
		if err = json.Unmarshal(item.Status.Raw, temp); err != nil {
			return nil, err
		}
		klog.V(3).Infof("Grab daemonset(%s/%s) status from cluster(%s), desired: %d, ready: %d, updated: %d, available: %d, unavailable: %d",
			ds.Namespace, ds.Name, item.ClusterName, temp.DesiredNumberScheduled, temp.NumberAvailable, temp.UpdatedNumberScheduled, temp.NumberAvailable, temp.NumberUnavailable)
		newStatus.ObservedGeneration = ds.Generation
		newStatus.DesiredNumberScheduled += temp.DesiredNumberScheduled
		newStatus.CurrentNumberScheduled += temp.CurrentNumberScheduled
		newStatus.UpdatedNumberScheduled += temp.UpdatedNumberScheduled
		newStatus.NumberMisscheduled += temp.NumberMisscheduled
		newStatus.NumberAvailable += temp.NumberAvailable
		newStatus.NumberUnavailable += temp.NumberUnavailable
		newStatus.NumberReady += temp.NumberReady
		if len(aggregatedStatusItems) == 1 {
			newStatus.Conditions = temp.Conditions
		}
	}

	if !reflect.DeepEqual(ds.Status, *newStatus) {
		klog.V(3).Infof("ignore update daemonset(%s/%s) status as up to date", ds.Namespace, ds.Name)
		return object, nil
	}

	return helper.ToUnstructured(ds)
}

func aggregatePVCStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	pvc, err := helper.ConvertToPVC(object)
	if err != nil {
		return nil, err
	}

	newStatus := &corev1.PersistentVolumeClaimStatus{}
	if len(aggregatedStatusItems) == 1 {
		item := aggregatedStatusItems[0]
		if item.Status == nil {
			return object, nil
		}
		if err = json.Unmarshal(item.Status.Raw, newStatus); err != nil {
			return nil, err
		}
		klog.V(3).Infof("Grab pvc(%s/%s) status from cluster(%s), phase: %s",
			pvc.Namespace, pvc.Name, item.ClusterName, newStatus.Phase)

	}

	if !reflect.DeepEqual(pvc.Status, *newStatus) {
		klog.V(3).Infof("ignore update pvc(%s/%s) status as up to date", pvc.Namespace, pvc.Name)
		return object, nil
	}

	return helper.ToUnstructured(pvc)
}

func aggregateVolumeSnapshotStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	snap, err := helper.ConvertToVolumeSnapshot(object)
	if err != nil {
		return nil, err
	}

	newStatus := &snapv1.VolumeSnapshotStatus{}
	if len(aggregatedStatusItems) == 1 {
		item := aggregatedStatusItems[0]
		if item.Status == nil {
			return object, nil
		}
		if err = json.Unmarshal(item.Status.Raw, newStatus); err != nil {
			return nil, err
		}
		klog.V(3).Infof("Grab volumesnapshot(%s/%s) status from cluster(%s), ready: %t",
			snap.Namespace, snap.Name, item.ClusterName, newStatus.ReadyToUse)
	} else {
		return object, nil
	}

	if !reflect.DeepEqual(snap.Status, *newStatus) {
		klog.V(3).Infof("ignore update pvc(%s/%s) status as up to date", snap.Namespace, snap.Name)
		return object, nil
	}

	return helper.ToUnstructured(snap)
}

func aggregateHPAStatus(object *unstructured.Unstructured, aggregatedStatusItems []workv1alpha2.AggregatedStatusItem) (*unstructured.Unstructured, error) {
	hpa, err := helper.ConvertToHPA(object)
	if err != nil {
		return nil, err
	}

	newStatus := &asv1.HorizontalPodAutoscalerStatus{}
	if len(aggregatedStatusItems) == 1 {
		item := aggregatedStatusItems[0]
		if item.Status == nil {
			return object, nil
		}
		if err = json.Unmarshal(item.Status.Raw, newStatus); err != nil {
			return nil, err
		}
		klog.V(3).Infof("Grab hpa(%s/%s) status from cluster(%s), current: %d, desired: %d",
			hpa.Namespace, hpa.Name, item.ClusterName, newStatus.CurrentReplicas, newStatus.DesiredReplicas)
	} else {
		return object, nil
	}

	if !reflect.DeepEqual(hpa.Status, *newStatus) {
		klog.V(3).Infof("ignore update hpa(%s/%s) status as up to date", hpa.Namespace, hpa.Name)
		return object, nil
	}

	return helper.ToUnstructured(hpa)
}