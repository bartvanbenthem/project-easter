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
	BackendGroup        = "gateway.kgateway.dev"
	BackendVersion      = "v1alpha1"
	BackendKind         = "Backend"
	BackendFieldManager = "apigatewaybackend-operator"

	// conditionTypeAccepted is kgateway's own condition type reported on
	// Backend (and every other kgateway CRD) once it's been validated and
	// accepted into its config snapshot. See
	// pkg/kgateway/proxy_syncer in github.com/kgateway-dev/kgateway.
	conditionTypeAccepted = "Accepted"
)

// BackendGVK is the GroupVersionKind of the kgateway Backend this operator
// manages.
var BackendGVK = schema.GroupVersionKind{Group: BackendGroup, Version: BackendVersion, Kind: BackendKind}

// BackendAdapter drives a paas APIGatewayBackend onto a same-named kgateway
// Backend of type "Static". It implements
// reconciler.Adapter[paasv1alpha1.APIGatewayBackend, *paasv1alpha1.APIGatewayBackend].
type BackendAdapter struct{}

func (BackendAdapter) GVK() schema.GroupVersionKind { return BackendGVK }

// TargetName returns the name of the Backend generated for crName. One paas
// APIGatewayBackend maps to exactly one same-named upstream Backend.
func (BackendAdapter) TargetName(crName string) string { return crName }

func (BackendAdapter) ObjectKind() string   { return "Backend" }
func (BackendAdapter) FieldManager() string { return BackendFieldManager }

// BuildManifest builds the desired gateway.kgateway.dev/v1alpha1 Backend
// object for cr, ready to be applied via Server-Side Apply. Only the
// "static" backend type is built -- see the APIGatewayBackendSpec doc
// comment for why.
func (BackendAdapter) BuildManifest(cr *paasv1alpha1.APIGatewayBackend, name, namespace, ownerName string) *unstructured.Unstructured {
	spec := cr.Spec

	hosts := make([]any, 0, len(spec.Hosts))
	for _, h := range spec.Hosts {
		hosts = append(hosts, map[string]any{
			"host": h.Host,
			"port": int64(h.Port),
		})
	}

	static := map[string]any{"hosts": hosts}
	if spec.AppProtocol != "" {
		static["appProtocol"] = spec.AppProtocol
	}

	backendSpec := map[string]any{
		"static": static,
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(BackendGVK)
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": BackendFieldManager,
		"paas.example.com/owner":       ownerName,
	})
	u.Object["spec"] = backendSpec

	return u
}

// ExtractStatus pulls the "Accepted" condition out of a Backend's .status.
// A Backend has no instance/replica count of its own -- it's a routing
// target, not a workload -- so ObservedCount/ReadyCount are treated as a
// single logical unit (the Backend object itself) rather than a real count,
// the same degenerate shape used wherever a target's status carries only a
// boolean readiness signal.
func (BackendAdapter) ExtractStatus(u *unstructured.Unstructured) reconciler.TargetStatus {
	if u == nil {
		return reconciler.TargetStatus{Phase: phaseUnknown}
	}

	conditions, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	phase := phaseUnknown
	ready := false
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := cond["type"].(string); t != conditionTypeAccepted {
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

	readyCount := int32(0)
	if ready {
		readyCount = 1
	}

	return reconciler.TargetStatus{
		Phase:         phase,
		Ready:         ready,
		ObservedCount: 1,
		ReadyCount:    readyCount,
	}
}

// ApplyStatus mirrors s onto cr's own .status (a standard Ready condition).
func (BackendAdapter) ApplyStatus(cr *paasv1alpha1.APIGatewayBackend, targetName string, s reconciler.TargetStatus) string {
	var message string
	if s.Ready {
		message = fmt.Sprintf("Backend %q is accepted", targetName)
	} else {
		message = fmt.Sprintf("Waiting for Backend %q (phase: %s)", targetName, s.Phase)
	}

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "BackendNotReady",
		Message:            message,
		ObservedGeneration: cr.Generation,
	}
	if s.Ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "BackendReady"
	}
	meta.SetStatusCondition(&cr.Status.Conditions, condition)

	cr.Status.Ready = s.Ready
	cr.Status.Phase = s.Phase
	cr.Status.Message = message

	return message
}
