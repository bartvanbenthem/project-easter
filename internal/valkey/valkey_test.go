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

package valkey

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
)

func TestExtraResourcesExpose(t *testing.T) {
	baseSpec := paasv1alpha1.ValkeyClusterSpec{
		Shards:      1,
		Persistence: paasv1alpha1.PersistenceSpec{Size: "1Gi"},
	}

	t.Run("unset returns an absent entry", func(t *testing.T) {
		cr := &paasv1alpha1.ValkeyCluster{Spec: baseSpec}

		extras := Adapter{}.ExtraResources(cr, "test", "default", "test")
		if len(extras) != 1 {
			t.Fatalf("expected 1 extra, got %d", len(extras))
		}
		if extras[0].Desired != nil {
			t.Fatalf("expected Desired to be nil when Expose is unset")
		}
		if extras[0].Name != "test-external" {
			t.Fatalf("expected name test-external, got %q", extras[0].Name)
		}
	})

	t.Run("set mirrors valkey-operator's own cluster-selector label", func(t *testing.T) {
		spec := baseSpec
		spec.Expose = &paasv1alpha1.ServiceExposeSpec{Type: "LoadBalancer"}
		cr := &paasv1alpha1.ValkeyCluster{Spec: spec}

		extras := Adapter{}.ExtraResources(cr, "test", "default", "test")
		if len(extras) != 1 || extras[0].Desired == nil {
			t.Fatalf("expected exactly one desired extra, got %+v", extras)
		}

		svcType, _, _ := unstructured.NestedString(extras[0].Desired.Object, "spec", "type")
		if svcType != "LoadBalancer" {
			t.Fatalf("expected spec.type LoadBalancer, got %q", svcType)
		}
		selector, _, _ := unstructured.NestedString(extras[0].Desired.Object, "spec", "selector", "valkey.io/cluster")
		if selector != "test" {
			t.Fatalf("expected selector valkey.io/cluster=test, got %q", selector)
		}
	})
}
