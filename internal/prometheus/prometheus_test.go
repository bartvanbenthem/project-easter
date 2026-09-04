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

package prometheus

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
)

func TestBuildManifestNamespaceScoped(t *testing.T) {
	cr := &paasv1alpha1.PrometheusInstance{
		Spec: paasv1alpha1.PrometheusInstanceSpec{Replicas: 1},
	}

	u := Adapter{}.BuildManifest(cr, "test", "monitoring-ns", "test")

	if ns := u.GetNamespace(); ns != "monitoring-ns" {
		t.Fatalf("expected Prometheus namespace %q, got %q", "monitoring-ns", ns)
	}

	// A null/unset namespace selector is how the Prometheus Operator scopes
	// monitor discovery to the Prometheus's own namespace -- these keys must
	// never be set, or discovery would widen beyond that namespace.
	if _, found, _ := unstructured.NestedMap(u.Object, "spec", "serviceMonitorNamespaceSelector"); found {
		t.Fatalf("expected spec.serviceMonitorNamespaceSelector to be unset (namespace-scoped)")
	}
	if _, found, _ := unstructured.NestedMap(u.Object, "spec", "podMonitorNamespaceSelector"); found {
		t.Fatalf("expected spec.podMonitorNamespaceSelector to be unset (namespace-scoped)")
	}

	// The (namespace-scoped) selectors themselves must still be non-nil and
	// empty so every monitor in that single namespace is actually picked up.
	if _, found, _ := unstructured.NestedMap(u.Object, "spec", "serviceMonitorSelector"); !found {
		t.Fatalf("expected spec.serviceMonitorSelector to be set (empty selector)")
	}
	if _, found, _ := unstructured.NestedMap(u.Object, "spec", "podMonitorSelector"); !found {
		t.Fatalf("expected spec.podMonitorSelector to be set (empty selector)")
	}
}

func TestBuildManifestStorage(t *testing.T) {
	cr := &paasv1alpha1.PrometheusInstance{
		Spec: paasv1alpha1.PrometheusInstanceSpec{
			Replicas: 1,
			Storage:  &paasv1alpha1.StorageSpec{Size: "5Gi", StorageClass: "fast"},
		},
	}

	u := Adapter{}.BuildManifest(cr, "test", "default", "test")

	size, _, _ := unstructured.NestedString(u.Object, "spec", "storage", "volumeClaimTemplate", "spec", "resources", "requests", "storage")
	if size != "5Gi" {
		t.Fatalf("expected storage size 5Gi, got %q", size)
	}
	class, _, _ := unstructured.NestedString(u.Object, "spec", "storage", "volumeClaimTemplate", "spec", "storageClassName")
	if class != "fast" {
		t.Fatalf("expected storageClassName fast, got %q", class)
	}
}
