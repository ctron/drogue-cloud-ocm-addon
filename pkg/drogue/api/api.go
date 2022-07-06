package api

import (
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const DrogueCloudOcmFinalizer = "drogue-cloud-ocm"

type ScopedMetadata struct {
	Name        string `json:"name"`
	Application string `json:"application"`

	Annotations map[string]string `json:"annotations,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`

	CreationTimestamp string `json:"creationTimestamp"`
	DeletionTimestamp string `json:"deletionTimestamp,omitempty"`

	Finalizers      []string `json:"finalizers,omitempty"`
	Generation      int64    `json:"generation"`
	ResourceVersion string   `json:"resourceVersion"`
	Uid             string   `json:"uid"`
}

type Device struct {
	Metadata ScopedMetadata `json:"metadata"`

	Spec   map[string]json.RawMessage `json:"spec,omitempty"`
	Status map[string]json.RawMessage `json:"status,omitempty"`
}

// RemoveFinalizer removes the finalizer, unless it was already gone
func (m *ScopedMetadata) RemoveFinalizer(name string) bool {
	for i, f := range m.Finalizers {
		if f == name {
			m.Finalizers = append(m.Finalizers[:i], m.Finalizers[i+1:]...)
			return true
		}
	}

	return false
}

type KubernetesStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// CleanupDevice cleans up all information from the device link
func (device *Device) CleanupDevice() bool {

	changed := false

	if device.Metadata.RemoveFinalizer(DrogueCloudOcmFinalizer) {
		changed = true
	}

	if device.Status != nil {
		if device.Status["kubernetes"] != nil {
			delete(device.Status, "kubernetes")
			changed = true
		}
	}

	return changed

}
