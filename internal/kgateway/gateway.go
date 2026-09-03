// Package kgateway contains everything that talks to the standard
// Kubernetes Gateway API (github.com/kubernetes-sigs/gateway-api) and to
// kgateway's own gateway.kgateway.dev/v1alpha1 Backend
// (github.com/kgateway-dev/kgateway).
//
// Two paas CRDs map into this one package because they're two halves of one
// integration: APIGateway fronts the vendor-neutral, upstream
// gateway.networking.k8s.io/v1 Gateway (GatewayAdapter, this file) --
// implemented identically by kgateway, Envoy Gateway, Istio, cilium, etc.,
// selected here via spec.gatewayClassName defaulting to "kgateway" --  while
// APIGatewayBackend (BackendAdapter, backend.go) fronts kgateway's own
// Backend, which is what actually lets a route reach something outside the
// cluster (another Kubernetes cluster, a VM, any bare host:port) rather than
// only an in-cluster Service. See each type's doc comment in api/v1alpha1
// for the full rationale.
//
// As with internal/cnpg, internal/valkey, internal/grafana,
// internal/mariadb, and internal/rabbitmq, we deliberately do not vendor
// either vendor's own Go API types or CRD schema. Both targets are
// addressed purely through controller-runtime's dynamic client
// (unstructured.Unstructured), and the desired object is built as a plain
// map applied via Server-Side Apply. This keeps the operator decoupled from
// any specific Gateway API or kgateway version. See
// crd-gateway-api-v1.6.1.yaml and crd-kgateway-v2.4.4.yaml at the repo root
// for the schemas this was built against.
//
// GatewayAdapter and BackendAdapter each implement internal/reconciler's
// Adapter interface, so the actual reconciliation loop (finalizers, SSA,
// status-mirroring) lives once in internal/reconciler and is shared with
// every other vendor integration.
package kgateway

import (
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
	"github.com/bartvanbenthem/paas-operator/internal/reconciler"
)

const (
	GatewayGroup        = "gateway.networking.k8s.io"
	GatewayVersion      = "v1"
	GatewayKind         = "Gateway"
	GatewayFieldManager = "apigateway-operator"

	// defaultGatewayClassName is the GatewayClass name kgateway's own Helm
	// chart creates. Used when APIGatewaySpec.GatewayClassName is empty
	// (only relevant for a CR built without going through API server
	// defaulting, e.g. directly in Go -- the CRD's own
	// +kubebuilder:default already covers the normal admission path).
	defaultGatewayClassName = "kgateway"

	// conditionTypeProgrammed is the Gateway API's own condition type that
	// goes True once the data plane has actually been configured to serve
	// traffic per this Gateway (as opposed to "Accepted", which only means
	// the spec was accepted). See
	// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.GatewayConditionType
	conditionTypeProgrammed = "Programmed"

	// conditionStatusTrue and phaseUnknown are shared across this package's
	// two adapters (gateway.go, backend.go).
	conditionStatusTrue = "True"
	phaseUnknown        = "Unknown"
)

// GatewayGVK is the GroupVersionKind of the standard Gateway API Gateway
// this operator manages.
var GatewayGVK = schema.GroupVersionKind{Group: GatewayGroup, Version: GatewayVersion, Kind: GatewayKind}

// GatewayAdapter drives a paas APIGateway onto a same-named Gateway API
// Gateway. It implements
// reconciler.Adapter[paasv1alpha1.APIGateway, *paasv1alpha1.APIGateway].
type GatewayAdapter struct{}

func (GatewayAdapter) GVK() schema.GroupVersionKind { return GatewayGVK }

// TargetName returns the name of the Gateway generated for crName. One paas
// APIGateway maps to exactly one same-named upstream Gateway.
func (GatewayAdapter) TargetName(crName string) string { return crName }

func (GatewayAdapter) ObjectKind() string   { return "Gateway" }
func (GatewayAdapter) FieldManager() string { return GatewayFieldManager }

// BuildManifest builds the desired gateway.networking.k8s.io/v1 Gateway
// object for cr, ready to be applied via Server-Side Apply.
func (GatewayAdapter) BuildManifest(cr *paasv1alpha1.APIGateway, name, namespace, ownerName string) *unstructured.Unstructured {
	spec := cr.Spec

	className := spec.GatewayClassName
	if className == "" {
		className = defaultGatewayClassName
	}

	listeners := make([]any, 0, len(spec.Listeners))
	for _, l := range spec.Listeners {
		protocol := l.Protocol
		if protocol == "" {
			protocol = "HTTP"
		}

		listener := map[string]any{
			"name":     l.Name,
			"port":     int64(l.Port),
			"protocol": protocol,
			// Gateway API defaults allowedRoutes.namespaces.from to "Same",
			// requiring a ReferenceGrant for routes in any other namespace
			// to attach. Default to "All" instead: a shared API gateway is
			// normally meant to be a routing target for the whole cluster,
			// not just its own namespace.
			"allowedRoutes": map[string]any{
				"namespaces": map[string]any{"from": "All"},
			},
		}
		if l.Hostname != "" {
			listener["hostname"] = l.Hostname
		}
		if l.TLSSecretName != "" {
			listener["tls"] = map[string]any{
				"mode":            "Terminate",
				"certificateRefs": []any{map[string]any{"name": l.TLSSecretName}},
			}
		}
		listeners = append(listeners, listener)
	}

	gatewaySpec := map[string]any{
		"gatewayClassName": className,
		"listeners":        listeners,
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(GatewayGVK)
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": GatewayFieldManager,
		"paas.example.com/owner":       ownerName,
	})
	u.Object["spec"] = gatewaySpec

	return u
}

// ExtractStatus pulls the listener count and the "Programmed" condition out
// of a Gateway's .status/.spec. observedCount is read back from the applied
// .spec.listeners (the desired count) rather than .status.listeners, so a
// Gateway the implementation hasn't reconciled yet reads as "0 ready of N"
// instead of "0 of 0" (which would look identical to a Gateway with no
// listeners at all). readyCount counts .status.listeners entries whose own
// "Programmed" condition is True.
func (GatewayAdapter) ExtractStatus(u *unstructured.Unstructured) reconciler.TargetStatus {
	if u == nil {
		return reconciler.TargetStatus{Phase: phaseUnknown}
	}

	specListeners, _, _ := unstructured.NestedSlice(u.Object, "spec", "listeners")
	observed := int32(len(specListeners))

	conditions, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	phase := phaseUnknown
	ready := false
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := cond["type"].(string); t != conditionTypeProgrammed {
			continue
		}
		if s, _ := cond["status"].(string); s == conditionStatusTrue {
			ready = true
		}
		if r, _ := cond["reason"].(string); r != "" {
			phase = r
		}
		break
	}

	statusListeners, _, _ := unstructured.NestedSlice(u.Object, "status", "listeners")
	var readyCount int32
	for _, l := range statusListeners {
		listener, ok := l.(map[string]any)
		if !ok {
			continue
		}
		lConditions, _ := listener["conditions"].([]any)
		for _, c := range lConditions {
			cond, ok := c.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := cond["type"].(string); t != conditionTypeProgrammed {
				continue
			}
			if s, _ := cond["status"].(string); s == conditionStatusTrue {
				readyCount++
			}
			break
		}
	}

	return reconciler.TargetStatus{
		Phase:         phase,
		Ready:         ready && observed > 0,
		ObservedCount: observed,
		ReadyCount:    readyCount,
	}
}

// ApplyStatus mirrors s onto cr's own .status (listeners/readyListeners + a
// standard Ready condition).
func (GatewayAdapter) ApplyStatus(cr *paasv1alpha1.APIGateway, targetName string, s reconciler.TargetStatus) string {
	var message string
	if s.Ready {
		message = fmt.Sprintf("Gateway %q is programmed (%d/%d listeners ready)", targetName, s.ReadyCount, s.ObservedCount)
	} else {
		message = fmt.Sprintf("Waiting for Gateway %q (phase: %s)", targetName, s.Phase)
	}

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "GatewayNotReady",
		Message:            message,
		ObservedGeneration: cr.Generation,
	}
	if s.Ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "GatewayReady"
	}
	meta.SetStatusCondition(&cr.Status.Conditions, condition)

	cr.Status.Ready = s.Ready
	cr.Status.Phase = s.Phase
	cr.Status.Listeners = s.ObservedCount
	cr.Status.ReadyListeners = s.ReadyCount
	cr.Status.Message = message

	return message
}
