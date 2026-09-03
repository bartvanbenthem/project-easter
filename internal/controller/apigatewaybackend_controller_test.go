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

var _ = Describe("APIGatewayBackend Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-apigatewaybackend"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind APIGatewayBackend")
			apigatewaybackend := &paasv1alpha1.APIGatewayBackend{}
			err := k8sClient.Get(ctx, typeNamespacedName, apigatewaybackend)
			if err != nil && errors.IsNotFound(err) {
				resource := &paasv1alpha1.APIGatewayBackend{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: resourceNamespace,
					},
					Spec: paasv1alpha1.APIGatewayBackendSpec{
						Hosts: []paasv1alpha1.BackendHost{
							{Host: "svc.other-cluster.example.com", Port: 443},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup the specific resource instance APIGatewayBackend")
			resource := &paasv1alpha1.APIGatewayBackend{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
				// Reconcile the deletion path so the finalizer is removed and
				// the CR actually disappears, instead of being left stuck
				// terminating for the next test.
				controllerReconciler := &APIGatewayBackendReconciler{
					Client:   k8sClient,
					Scheme:   k8sClient.Scheme(),
					Recorder: events.NewFakeRecorder(10),
					Adapter:  kgateway.BackendAdapter{},
					Name:     APIGatewayBackendControllerName,
				}
				_, _ = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			}

			target := &unstructured.Unstructured{}
			target.SetGroupVersionKind(kgateway.BackendGVK)
			target.SetName(resourceName)
			target.SetNamespace(resourceNamespace)
			_ = k8sClient.Delete(ctx, target)
		})

		It("should create a matching Backend and report a Ready=False status", func() {
			controllerReconciler := &APIGatewayBackendReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events.NewFakeRecorder(10),
				Adapter:  kgateway.BackendAdapter{},
				Name:     APIGatewayBackendControllerName,
			}

			By("reconciling once to attach the finalizer")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			var withFinalizer paasv1alpha1.APIGatewayBackend
			Expect(k8sClient.Get(ctx, typeNamespacedName, &withFinalizer)).To(Succeed())
			Expect(withFinalizer.Finalizers).To(ContainElement("paas.example.com/cleanup"))

			By("reconciling again to apply the Backend and patch status")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the generated Backend matches the paas APIGatewayBackend spec")
			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(kgateway.BackendGVK)
			Expect(k8sClient.Get(ctx, typeNamespacedName, got)).To(Succeed())

			hosts, _, _ := unstructured.NestedSlice(got.Object, "spec", "static", "hosts")
			Expect(hosts).To(HaveLen(1))

			host, ok := hosts[0].(map[string]any)
			Expect(ok).To(BeTrue())
			Expect(host["host"]).To(Equal("svc.other-cluster.example.com"))
			Expect(host["port"]).To(Equal(int64(443)))

			By("verifying the paas APIGatewayBackend status was patched")
			var withStatus paasv1alpha1.APIGatewayBackend
			Expect(k8sClient.Get(ctx, typeNamespacedName, &withStatus)).To(Succeed())
			// No real kgateway controller runs in this envtest, so the
			// Backend never actually gets accepted — this asserts the
			// *mapping* (not-ready status correctly mirrored), not
			// kgateway's own behavior.
			Expect(withStatus.Status.Ready).To(BeFalse())
			cond := meta.FindStatusCondition(withStatus.Status.Conditions, "Ready")
			Expect(cond).NotTo(BeNil())
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		})
	})
})
