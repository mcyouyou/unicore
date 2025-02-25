/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ImageListSpec defines the desired state of ImageList.
type ImageListSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// map[ImageName]ImageInfo
	Images map[string]ImageInfo `json:"images"`
}

type ImageInfo struct {
	// if true, exec docker pull for this img every 24h
	AlwaysPull bool `json:"alwaysPull,omitempty"`
	// expected next pulling time if AlwaysPull is true
	NextPull metav1.Time `json:"nextPull,omitempty"`
}

// ImageListStatus defines the observed state of ImageList.
type ImageListStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ImageList is the Schema for the imagelists API.
// +genclient
type ImageList struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ImageListSpec   `json:"spec,omitempty"`
	Status ImageListStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ImageListList contains a list of ImageList.
type ImageListList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ImageList `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ImageList{}, &ImageListList{})
}
