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

// APIGatewayReconciler reconciles an APIGateway object. It is the shared
// reconciler.GenericReconciler engine wired to the standard Gateway API
// mapping in internal/kgateway — see internal/reconciler for the finalizer /
// Server-Side Apply / status-mirroring logic every vendor integration
// shares, and internal/kgateway for what's specific to the Gateway
// API/kgateway.
type APIGatewayReconciler = reconciler.GenericReconciler[paasv1alpha1.APIGateway, *paasv1alpha1.APIGateway]

// APIGatewayControllerName is the controller name used both when
// registering with the manager and in logs/events.
const APIGatewayControllerName = "apigateway"

// +kubebuilder:rbac:groups=paas.example.com,resources=apigateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=paas.example.com,resources=apigateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=paas.example.com,resources=apigateways/finalizers,verbs=update
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// NewAPIGatewayReconciler builds the APIGateway controller.
func NewAPIGatewayReconciler(c client.Client, scheme *runtime.Scheme, recorder events.EventRecorder) *APIGatewayReconciler {
	return &APIGatewayReconciler{
		Client:   c,
		Scheme:   scheme,
		Recorder: recorder,
		Adapter:  kgateway.GatewayAdapter{},
		Name:     APIGatewayControllerName,
	}
}
