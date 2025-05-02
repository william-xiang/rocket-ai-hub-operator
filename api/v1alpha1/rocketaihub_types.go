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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RocketAIHubSpec defines the desired state of RocketAIHub
type RocketAIHubSpec struct {
	// Components defines the components to be installed
	Components Components `json:"components,omitempty"`
}

type Components struct {
	Katib             Katib             `json:"katib,omitempty"`
	KServe            Kserve            `json:"kServe,omitempty"`
	KubeflowPipelines KubeflowPipelines `json:"kubeflowPipelines,omitempty"`
	TrainingOperator  TrainingOperator  `json:"trainingOperator,omitempty"`
	KubeflowNotebooks KubeflowNotebooks `json:"kubeflowNotebooks,omitempty"`
}

type Katib struct {
	Component `json:""`
}

type Kserve struct {
	Component `json:""`
}

type KubeflowPipelines struct {
	Component `json:""`
}

type TrainingOperator struct {
	Component `json:""`
}

type KubeflowNotebooks struct {
	Component `json:""`
}

type Component struct {
	// +kubebuilder:default=true
	// +kubebuilder:validation:Required
	Install bool `json:"install,omitempty"`
}

// RocketAIHubStatus defines the observed state of RocketAIHub
type RocketAIHubStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// RocketAIHub is the Schema for the rocketaihubs API
type RocketAIHub struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RocketAIHubSpec   `json:"spec,omitempty"`
	Status RocketAIHubStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RocketAIHubList contains a list of RocketAIHub
type RocketAIHubList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RocketAIHub `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RocketAIHub{}, &RocketAIHubList{})
}
