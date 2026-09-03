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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
	"github.com/bartvanbenthem/paas-operator/internal/kgateway"
)

var _ = Describe("APIGateway Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-apigateway"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind APIGateway")
			apigateway := &paasv1alpha1.APIGateway{}
			err := k8sClient.Get(ctx, typeNamespacedName, apigateway)
			if err != nil && errors.IsNotFound(err) {
				resource := &paasv1alpha1.APIGateway{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: paasv1alpha1.APIGatewaySpec{
						Listeners: []paasv1alpha1.GatewayListener{
							{Name: "http", Port: 80, Protocol: "HTTP"},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance APIGateway")
			resource := &paasv1alpha1.APIGateway{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
				// Reconcile the deletion path so the finalizer is removed and
				// the CR actually disappears, instead of being left stuck
				// terminating for the next test.
				controllerReconciler := &APIGatewayReconciler{
					Client:   k8sClient,
					Scheme:   k8sClient.Scheme(),
					Recorder: events.NewFakeRecorder(10),
					Adapter:  kgateway.GatewayAdapter{},
					Name:     APIGatewayControllerName,
				}
				_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			}

			target := &unstructured.Unstructured{}
			target.SetGroupVersionKind(kgateway.GatewayGVK)
			target.SetName(resourceName)
			target.SetNamespace(resourceNamespace)
			_ = k8sClient.Delete(ctx, target)
		})

		It("should create a matching Gateway and report a Ready=False status", func() {
			controllerReconciler := &APIGatewayReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events.NewFakeRecorder(10),
				Adapter:  kgateway.GatewayAdapter{},
				Name:     APIGatewayControllerName,
			}

			By("reconciling once to attach the finalizer")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var withFinalizer paasv1alpha1.APIGateway
			Expect(k8sClient.Get(ctx, typeNamespacedName, &withFinalizer)).To(Succeed())
			Expect(withFinalizer.Finalizers).To(ContainElement("paas.example.com/cleanup"))
			// The CRD default should have applied on admission.
			Expect(withFinalizer.Spec.GatewayClassName).To(Equal("kgateway"))

			By("reconciling again to apply the Gateway and patch status")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the generated Gateway matches the paas APIGateway spec")
			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(kgateway.GatewayGVK)
			Expect(k8sClient.Get(ctx, typeNamespacedName, got)).To(Succeed())

			className, _, _ := unstructured.NestedString(got.Object, "spec", "gatewayClassName")
			Expect(className).To(Equal("kgateway"))

			listeners, _, _ := unstructured.NestedSlice(got.Object, "spec", "listeners")
			Expect(listeners).To(HaveLen(1))

			listener, ok := listeners[0].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(listener["port"]).To(Equal(int64(80)))
			Expect(listener["protocol"]).To(Equal("HTTP"))

			allowedRoutes, ok := listener["allowedRoutes"].(map[string]any)
			Expect(ok).To(BeTrue())
			namespaces, ok := allowedRoutes["namespaces"].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(namespaces["from"]).To(Equal("All"))

			By("verifying the paas APIGateway status was patched")
			var withStatus paasv1alpha1.APIGateway
			Expect(k8sClient.Get(ctx, typeNamespacedName, &withStatus)).To(Succeed())
			// No real Gateway API implementation runs in this envtest, so
			// the Gateway never actually gets programmed — this asserts the
			// *mapping* (not-ready status correctly mirrored), not
			// kgateway's own behavior.
			Expect(withStatus.Status.Ready).To(BeFalse())
			cond := meta.FindStatusCondition(withStatus.Status.Conditions, "Ready")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})
	})
})
