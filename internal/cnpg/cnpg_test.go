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

package cnpg

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
)

func TestBuildManifestMonitoring(t *testing.T) {
	baseSpec := paasv1alpha1.PostgresClusterSpec{
		Instances: 1,
		Storage:   paasv1alpha1.StorageSpec{Size: "1Gi"},
		Database:  paasv1alpha1.DatabaseSpec{Name: "app", Owner: "app"},
	}

	t.Run("disabled by default", func(t *testing.T) {
		cr := &paasv1alpha1.PostgresCluster{Spec: baseSpec}
		u := Adapter{}.BuildManifest(cr, "test", "default", "test")

		if _, found, _ := unstructured.NestedMap(u.Object, "spec", "monitoring"); found {
			t.Fatalf("expected no spec.monitoring when EnablePodMonitor is unset")
		}
	})

	t.Run("enabled sets namespace-scoped PodMonitor", func(t *testing.T) {
		spec := baseSpec
		spec.Monitoring = paasv1alpha1.MonitoringSpec{EnablePodMonitor: true}
		cr := &paasv1alpha1.PostgresCluster{Spec: spec}

		u := Adapter{}.BuildManifest(cr, "test", "default", "test")

		enabled, found, err := unstructured.NestedBool(u.Object, "spec", "monitoring", "enablePodMonitor")
		if err != nil {
			t.Fatalf("NestedBool: %v", err)
		}
		if !found || !enabled {
			t.Fatalf("expected spec.monitoring.enablePodMonitor=true, got found=%v enabled=%v", found, enabled)
		}

		// CNPG always creates the PodMonitor in the same namespace as the
		// Cluster itself, so the generated Cluster's own namespace is what
		// makes this monitoring namespace-scoped.
		if ns := u.GetNamespace(); ns != "default" {
			t.Fatalf("expected Cluster namespace %q, got %q", "default", ns)
		}
	})
}
