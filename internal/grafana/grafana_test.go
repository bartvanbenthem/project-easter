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

package grafana

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
)

func TestBuildManifestIngress(t *testing.T) {
	t.Run("unset leaves spec.ingress unset", func(t *testing.T) {
		cr := &paasv1alpha1.GrafanaInstance{Spec: paasv1alpha1.GrafanaInstanceSpec{Replicas: 1}}
		u := Adapter{}.BuildManifest(cr, "test", "default", "test")

		if _, found, _ := unstructured.NestedMap(u.Object, "spec", "ingress"); found {
			t.Fatalf("expected no spec.ingress when Ingress is unset")
		}
	})

	t.Run("set fills the underlying Grafana's own spec.ingress", func(t *testing.T) {
		cr := &paasv1alpha1.GrafanaInstance{
			Spec: paasv1alpha1.GrafanaInstanceSpec{
				Replicas: 1,
				Ingress: &paasv1alpha1.IngressSpec{
					Host:             "grafana.example.com",
					IngressClassName: "nginx",
					TLSSecretName:    "grafana-tls",
					Annotations:      map[string]string{"cert-manager.io/cluster-issuer": "letsencrypt"},
				},
			},
		}
		u := Adapter{}.BuildManifest(cr, "test", "default", "test")

		rules, found, _ := unstructured.NestedSlice(u.Object, "spec", "ingress", "spec", "rules")
		if !found || len(rules) != 1 {
			t.Fatalf("expected exactly one ingress rule, found=%v len=%d", found, len(rules))
		}
		rule, _ := rules[0].(map[string]any)
		if host, _, _ := unstructured.NestedString(rule, "host"); host != "grafana.example.com" {
			t.Fatalf("expected host grafana.example.com, got %q", host)
		}

		class, _, _ := unstructured.NestedString(u.Object, "spec", "ingress", "spec", "ingressClassName")
		if class != "nginx" {
			t.Fatalf("expected ingressClassName nginx, got %q", class)
		}

		tls, _, _ := unstructured.NestedSlice(u.Object, "spec", "ingress", "spec", "tls")
		if len(tls) != 1 {
			t.Fatalf("expected exactly one tls entry, got %d", len(tls))
		}
		tlsEntry, _ := tls[0].(map[string]any)
		secretName, _, _ := unstructured.NestedString(tlsEntry, "secretName")
		if secretName != "grafana-tls" {
			t.Fatalf("expected tls secretName grafana-tls, got %q", secretName)
		}

		ann, _, _ := unstructured.NestedString(u.Object, "spec", "ingress", "metadata", "annotations", "cert-manager.io/cluster-issuer")
		if ann != "letsencrypt" {
			t.Fatalf("expected annotation to be copied through, got %q", ann)
		}
	})
}
