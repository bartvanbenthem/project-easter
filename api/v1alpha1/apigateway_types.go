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

// APIGatewaySpec defines the desired state of APIGateway.
//
// It is a minimal, opinionated front for a standard Kubernetes Gateway API
// gateway.networking.k8s.io/v1 Gateway: the operator creates and manages a
// same-named Gateway from this spec. Gateway itself is a vendor-neutral,
// upstream Kubernetes API (implemented identically by kgateway, Envoy
// Gateway, Istio, cilium, ...); gatewayClassName is what selects the
// implementation actually provisioning it, and defaults here to "kgateway"
// (github.com/kgateway-dev/kgateway) -- see APIGatewayBackend for the piece
// that's kgateway-specific: routing to a backend outside the cluster (a
// resource on another cluster, another runtime, a bare host:port) rather
// than only to an in-cluster Service, which plain Gateway API HTTPRoute
// alone cannot do. See internal/kgateway for the mapping.
type APIGatewaySpec struct {
	// gatewayClassName selects the Gateway API implementation that
	// provisions this Gateway. Defaults to "kgateway", the GatewayClass
	// name kgateway's own Helm chart creates.
	// +kubebuilder:default=kgateway
	// +optional
	GatewayClassName string `json:"gatewayClassName,omitempty"`

	// listeners are the network entry points exposed by this Gateway. At
	// least one is required.
	// +kubebuilder:validation:MinItems=1
	// +required
	Listeners []GatewayListener `json:"listeners"`
}

// GatewayListener is one network entry point on an APIGateway.
type GatewayListener struct {
	// name of the listener; must be unique within the Gateway.
	// +required
	Name string `json:"name"`

	// port the listener binds to.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +required
	Port int32 `json:"port"`

	// protocol the listener speaks.
	// +kubebuilder:validation:Enum=HTTP;HTTPS;TLS;TCP;UDP
	// +kubebuilder:default=HTTP
	// +optional
	Protocol string `json:"protocol,omitempty"`

	// hostname the listener matches on. Leave unset to match any hostname.
	// +optional
	Hostname string `json:"hostname,omitempty"`

	// tlsSecretName is the name of a same-namespace Secret (type
	// kubernetes.io/tls) providing the certificate for this listener.
	// Required when protocol is "HTTPS" or "TLS"; invalid otherwise.
	// +optional
	TLSSecretName string `json:"tlsSecretName,omitempty"`
}

// APIGatewayStatus defines the observed state of APIGateway.
//
// It mirrors the subset of the underlying Gateway's status that matters for
// "is my gateway serving traffic yet".
type APIGatewayStatus struct {
	// ready is true once the underlying Gateway's "Programmed" condition
	// (the data plane has been configured to serve traffic) is True.
	// +optional
	Ready bool `json:"ready,omitempty"`

	// phase is the reason of the underlying Gateway's "Programmed"
	// condition (Gateway API has no separate status.phase field).
	// +optional
	Phase string `json:"phase,omitempty"`

	// listeners is the number of listeners in spec.listeners.
	// +optional
	Listeners int32 `json:"listeners,omitempty"`

	// readyListeners is the number of the underlying Gateway's
	// status.listeners entries whose own "Programmed" condition is True.
	// +optional
	ReadyListeners int32 `json:"readyListeners,omitempty"`

	// message is a short human-readable summary of the current state.
	// +optional
	Message string `json:"message,omitempty"`

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the APIGateway resource.
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
// +kubebuilder:resource:shortName=agw
// +kubebuilder:printcolumn:name="Class",type=string,JSONPath=`.spec.gatewayClassName`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// APIGateway is the Schema for the apigateways API
type APIGateway struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of APIGateway
	// +required
	Spec APIGatewaySpec `json:"spec"`

	// status defines the observed state of APIGateway
	// +optional
	Status APIGatewayStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// APIGatewayList contains a list of APIGateway
type APIGatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []APIGateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &APIGateway{}, &APIGatewayList{})
		return nil
	})
}
