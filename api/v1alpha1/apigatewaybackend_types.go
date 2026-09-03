/*
Copyright 2026.

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
	"k8s.io/apimachinery/pkg/runtime"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// APIGatewayBackendSpec defines the desired state of APIGatewayBackend.
//
// It is a minimal, opinionated front for a kgateway
// gateway.kgateway.dev/v1alpha1 Backend of type "Static": the operator
// creates and manages a same-named Backend from this spec. This is the
// piece that's actually kgateway-specific rather than standard Gateway
// API: a Static Backend routes to one or more bare host:port endpoints --
// a Service on another Kubernetes cluster, a VM, a managed service,
// anything reachable by network address -- rather than only to a Service in
// this cluster, which is as far as plain Gateway API HTTPRoute backendRefs
// go on their own. Only the "static" backend type is exposed; kgateway's
// AWS/GCP/dynamic-forward-proxy/priority-group Backend types are out of
// scope. Bind a path on an APIGateway to this Backend with a plain
// gateway.networking.k8s.io/v1 HTTPRoute (backendRefs: group
// gateway.kgateway.dev, kind Backend, name <this CR's name>) -- routing
// rules aren't wrapped here, the same way this operator's other
// integrations leave routing/child config to the vendor's own objects. See
// internal/kgateway for the mapping.
type APIGatewayBackendSpec struct {
	// hosts are the static host:port endpoints this Backend routes to. At
	// least one is required.
	// +kubebuilder:validation:MinItems=1
	// +required
	Hosts []BackendHost `json:"hosts"`

	// appProtocol is the application protocol to use when communicating
	// with the backend. Leave unset for plain HTTP/1.1.
	// +kubebuilder:validation:Enum=http2;grpc;grpc-web;kubernetes.io/h2c;kubernetes.io/ws
	// +optional
	AppProtocol string `json:"appProtocol,omitempty"`
}

// BackendHost is one static host:port endpoint of an APIGatewayBackend.
type BackendHost struct {
	// host is the hostname or IP address of the endpoint.
	// +required
	Host string `json:"host"`

	// port is the port of the endpoint.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +required
	Port int32 `json:"port"`
}

// APIGatewayBackendStatus defines the observed state of APIGatewayBackend.
//
// It mirrors the subset of the underlying Backend's status that matters for
// "is this backend usable yet". A Backend has no instance/replica count of
// its own to mirror the way PostgresCluster/MariaDBCluster/RabbitMQCluster
// do -- it's a routing target, not a workload -- so this only carries a
// readiness condition.
type APIGatewayBackendStatus struct {
	// ready is true once the underlying Backend's "Accepted" condition is
	// True.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// phase is the reason of the underlying Backend's "Accepted" condition
	// (kgateway has no separate status.phase field).
	// +optional
	Phase string `json:"phase,omitempty"`

	// message is a short human-readable summary of the current state.
	// +optional
	Message string `json:"message,omitempty"`

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the APIGatewayBackend resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=agwb
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// APIGatewayBackend is the Schema for the apigatewaybackends API
type APIGatewayBackend struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of APIGatewayBackend
	// +required
	Spec APIGatewayBackendSpec `json:"spec"`

	// status defines the observed state of APIGatewayBackend
	// +optional
	Status APIGatewayBackendStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// APIGatewayBackendList contains a list of APIGatewayBackend
type APIGatewayBackendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []APIGatewayBackend `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &APIGatewayBackend{}, &APIGatewayBackendList{})
		return nil
	})
}
