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

package controller

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
	"github.com/bartvanbenthem/paas-operator/internal/kgateway"
	"github.com/bartvanbenthem/paas-operator/internal/reconciler"
)

// APIGatewayBackendReconciler reconciles an APIGatewayBackend object. It is
// the shared reconciler.GenericReconciler engine wired to the kgateway
// Backend mapping in internal/kgateway — see internal/reconciler for the
// finalizer / Server-Side Apply / status-mirroring logic every vendor
// integration shares, and internal/kgateway for what's specific to
// kgateway.
type APIGatewayBackendReconciler = reconciler.GenericReconciler[paasv1alpha1.APIGatewayBackend, *paasv1alpha1.APIGatewayBackend]

// APIGatewayBackendControllerName is the controller name used both when
// registering with the manager and in logs/events.
const APIGatewayBackendControllerName = "apigatewaybackend"

// +kubebuilder:rbac:groups=paas.example.com,resources=apigatewaybackends,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=paas.example.com,resources=apigatewaybackends/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=paas.example.com,resources=apigatewaybackends/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.kgateway.dev,resources=backends,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// NewAPIGatewayBackendReconciler builds the APIGatewayBackend controller.
func NewAPIGatewayBackendReconciler(c client.Client, scheme *runtime.Scheme, recorder events.EventRecorder) *APIGatewayBackendReconciler {
	return &APIGatewayBackendReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: recorder,
		Adapter:  kgateway.BackendAdapter{},
		Name:     APIGatewayBackendControllerName,
	}
}
