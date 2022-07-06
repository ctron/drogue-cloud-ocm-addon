package agent

import (
	"encoding/json"
	"github.com/drogue-cloud/drogue-cloud-ocm-addon/pkg/config"
	"github.com/drogue-cloud/drogue-cloud-ocm-addon/pkg/drogue/api"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"open-cluster-management.io/addon-framework/pkg/agent"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"reflect"

	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

type drogueCloudAgent struct {
	config *config.DeviceSynchronizerConfig
	client *api.Client
}

const DeviceLinkedCondition = "DeviceLinked"

// ensure that this type implements agent.AgentAddon
var _ agent.AgentAddon = &drogueCloudAgent{}

type DrogueCloudAgent interface {
	agent.AgentAddon
}

func NewAgent(config *config.DeviceSynchronizerConfig) (DrogueCloudAgent, error) {
	client, err := api.New(config.API.URL, config.Credentials.Username, config.Credentials.Token)
	if err != nil {
		return nil, err
	}

	return &drogueCloudAgent{
		config: config,
		client: client,
	}, nil
}

// Manifests return the workload which should be run on the target device/cluster.
//
// If the workload is a pod/job flagged with the pre-delete marker, then this workload will be run before the addon
// is actually deleted.
//
// FIXME: leverage pre-delete hooks to remove the finalizer on the drogue cloud side
func (h *drogueCloudAgent) Manifests(cluster *clusterv1.ManagedCluster, addon *addonapiv1alpha1.ManagedClusterAddOn) ([]runtime.Object, error) {

	klog.Infof("Reconciling device: %v/%v", h.config.Application, addon.Namespace)

	// FIXME: all changes we made here on the addon will be lost, as this state is not persisted
	device, err := h.evalState(addon)

	if err != nil {
		return nil, err
	}
	if device == nil {
		// device not found
		// FIXME: consider creating one
		klog.Infof("Device %s not found", addon.Name)
		return nil, nil
	}

	if device.Metadata.DeletionTimestamp != "" {
		// deleted in Drogue Cloud, we don't schedule any workload
		klog.Infoln("Device deleted in Drogue Cloud, no longer scheduling workload")
		return nil, nil
	} else {
		// mirror addon conditions, cleanup is done in evalState
		if addon.DeletionTimestamp == nil {
			status := api.KubernetesStatus{
				Conditions: addon.Status.Conditions,
			}
			statusData, err := json.Marshal(status)
			if device.Status == nil {
				device.Status = map[string]json.RawMessage{}
			}
			if err != nil {
				return nil, err
			}
			if !reflect.DeepEqual(device.Status["kubernetes"], statusData) {
				device.Status["kubernetes"] = statusData
				if err := h.client.UpdateDevice(device); err != nil {
					klog.Warningf("Failed to sync conditions: %v", err)
					return nil, err
				}
			}
		}

	}

	return unmarshalPayload(device)
}

// evalState evaluates the current state
//
// NOTE: This function might change the addon CR, so it needs to be checked and updated if changed
func (h *drogueCloudAgent) evalState(addon *addonapiv1alpha1.ManagedClusterAddOn) (*api.Device, error) {
	device, err := h.client.GetDevice(h.config.Application, addon.Namespace)
	if err != nil {
		return nil, err
	}

	changed := false

	if device == nil {
		// device not found

		linkCondition := metav1.Condition{
			Type:               DeviceLinkedCondition,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 0,
			Reason:             "DeviceMissing",
			Message:            "Device is missing in the Drogue IoT cloud registry",
		}
		meta.SetStatusCondition(&addon.Status.Conditions, linkCondition)

		return nil, nil
	}

	if device.Metadata.DeletionTimestamp == "" && addon.DeletionTimestamp == nil {

		// device exists on both side, neither side is being deleted
		contains := false
		for _, f := range device.Metadata.Finalizers {
			if f == api.DrogueCloudOcmFinalizer {
				contains = true
				break
			}
		}

		if !contains {
			device.Metadata.Finalizers = append(device.Metadata.Finalizers, api.DrogueCloudOcmFinalizer)
			changed = true
		}

		linkCondition := metav1.Condition{
			Type:               DeviceLinkedCondition,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 0,
			Reason:             "AsExpected",
			Message:            "Device is linked to the cluster addon resource",
		}
		meta.SetStatusCondition(&addon.Status.Conditions, linkCondition)

	} else {

		// either side is being deleted, release by removing the finalizer
		changed = device.Metadata.RemoveFinalizer(api.DrogueCloudOcmFinalizer)

		var reason string
		var message string

		if device.Metadata.DeletionTimestamp != "" {
			reason = "DeviceDeleted"
			message = "Device is being deleted in Drogue IoT cloud"
		} else {
			reason = "AddonRemoved"
			message = "Cluster addon is being removed"
		}

		linkCondition := metav1.Condition{
			Type:               DeviceLinkedCondition,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 0,
			Reason:             reason,
			Message:            message,
		}
		meta.SetStatusCondition(&addon.Status.Conditions, linkCondition)

	}

	if addon.DeletionTimestamp != nil {
		if device.CleanupDevice() {
			changed = true
		}
	}

	if changed {
		// drogue device has changed
		if err := h.client.UpdateDevice(device); err != nil {
			klog.Warningf("Failed to update device: %v", err)
			return nil, err
		}
	}

	return device, nil

}

func (h *drogueCloudAgent) GetAgentAddonOptions() agent.AgentAddonOptions {
	return agent.AgentAddonOptions{
		AddonName: "drogue-cloud",
		InstallStrategy: &agent.InstallStrategy{
			Type:             agent.InstallByLabel,
			InstallNamespace: "drogue-iot",
			LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{
				"drogue-iot": "device",
			}},
		},
		HealthProber: &agent.HealthProber{Type: agent.HealthProberTypeWork},
	}
}

func unmarshalPayload(device *api.Device) ([]runtime.Object, error) {
	result := make([]unstructured.Unstructured, 0)

	k8s := device.Spec["kubernetes"]
	if k8s != nil {
		if err := json.Unmarshal(k8s, &result); err != nil {
			klog.Warningf("Failed to unmarshal k8s payload: %v", err)
			// FIXME: record as condition
			return nil, nil
		}
	}

	klog.Infof("K8S payload: %+v", result)

	r := make([]runtime.Object, len(result))
	for i, o := range result {
		// make a copy, because go, you know
		co := o
		r[i] = &co
	}

	return r, nil
}
