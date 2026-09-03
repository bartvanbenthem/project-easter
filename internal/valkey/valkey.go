// Package valkey contains everything that talks to the valkey-operator's
// valkey.io/v1alpha1 ValkeyCluster (github.com/valkey-io/valkey-operator).
//
// As with internal/cnpg, we deliberately do not vendor valkey-operator's own
// Go API types or its CRD schema. ValkeyCluster is addressed purely through
// controller-runtime's dynamic client (unstructured.Unstructured), and the
// desired object is built as a plain map applied via Server-Side Apply.
// This keeps the operator decoupled from any specific valkey-operator
// version. See crd-valkey-v0.6.0.yaml at the repo root for the schema this
// was built against.
//
// ValkeyCluster's own child ValkeyNode CRD is not targeted here: it is an
// internal object of the valkey-operator ("users should not create
// ValkeyNodes directly" per its own CRD description), so it plays the same
// role CNPG's internal-only resources do -- out of scope for this operator.
//
// Adapter implements internal/reconciler's Adapter interface, so the actual
// reconciliation loop (finalizers, SSA, status-mirroring) lives once in
// internal/reconciler and is shared with every other vendor integration.
package valkey

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
	"github.com/bartvanbenthem/paas-operator/internal/reconciler"
)

const (
	Group        = "valkey.io"
	Version      = "v1alpha1"
	Kind         = "ValkeyCluster"
	FieldManager = "valkeycluster-operator"
)

// GVK is the GroupVersionKind of the valkey-operator ValkeyCluster this
// operator manages.
var GVK = schema.GroupVersionKind{Group: Group, Version: Version, Kind: Kind}

// Adapter drives a paas ValkeyCluster onto a same-named valkey-operator
// ValkeyCluster. It implements
// reconciler.Adapter[paasv1alpha1.ValkeyCluster, *paasv1alpha1.ValkeyCluster].
type Adapter struct{}

func (Adapter) GVK() schema.GroupVersionKind { return GVK }

// TargetName returns the name of the ValkeyCluster generated for crName. One
// paas ValkeyCluster maps to exactly one same-named upstream ValkeyCluster.
func (Adapter) TargetName(crName string) string { return crName }

func (Adapter) ObjectKind() string   { return "Valkey ValkeyCluster" }
func (Adapter) FieldManager() string { return FieldManager }

func resourceListJSON(list corev1.ResourceList) map[string]any {
	if len(list) == 0 {
		return nil
	}
	out := map[string]any{}
	if cpu, ok := list[corev1.ResourceCPU]; ok {
		out["cpu"] = cpu.String()
	}
	if mem, ok := list[corev1.ResourceMemory]; ok {
		out["memory"] = mem.String()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// BuildManifest builds the desired valkey.io/v1alpha1 ValkeyCluster object
// for cr, ready to be applied via Server-Side Apply.
func (Adapter) BuildManifest(cr *paasv1alpha1.ValkeyCluster, name, namespace, ownerName string) *unstructured.Unstructured {
	spec := cr.Spec

	persistence := map[string]any{"size": spec.Persistence.Size}
	if spec.Persistence.StorageClass != "" {
		persistence["storageClassName"] = spec.Persistence.StorageClass
	}

	clusterSpec := map[string]any{
		"shards":      int64(spec.Shards),
		"replicas":    int64(spec.Replicas),
		"persistence": persistence,
	}

	if spec.Image != "" {
		clusterSpec["image"] = spec.Image
	}

	resources := map[string]any{}
	if requests := resourceListJSON(spec.Resources.Requests); requests != nil {
		resources["requests"] = requests
	}
	if limits := resourceListJSON(spec.Resources.Limits); limits != nil {
		resources["limits"] = limits
	}
	if len(resources) > 0 {
		clusterSpec["resources"] = resources
	}

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(GVK)
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": FieldManager,
		"paas.example.com/owner":       ownerName,
	})
	u.Object["spec"] = clusterSpec

	return u
}

// ExtractStatus pulls state/shards/readyShards out of a ValkeyCluster's
// .status. ready is derived from shard counts rather than the free-form
// state string, since the exact wording of state is not a stable API
// contract across valkey-operator versions.
func (Adapter) ExtractStatus(u *unstructured.Unstructured) reconciler.TargetStatus {
	if u == nil {
		return reconciler.TargetStatus{Phase: "Unknown"}
	}

	state, found, _ := unstructured.NestedString(u.Object, "status", "state")
	if !found || state == "" {
		state = "Unknown"
	}
	shards, _, _ := unstructured.NestedInt64(u.Object, "status", "shards")
	readyShards, _, _ := unstructured.NestedInt64(u.Object, "status", "readyShards")
	observed := int32(shards)
	ready := int32(readyShards)

	return reconciler.TargetStatus{
		Phase:         state,
		Ready:         observed > 0 && ready >= observed,
		ObservedCount: observed,
		ReadyCount:    ready,
	}
}

// ApplyStatus mirrors s onto cr's own .status (shards/readyShards + a
// standard Ready condition).
func (Adapter) ApplyStatus(cr *paasv1alpha1.ValkeyCluster, targetName string, s reconciler.TargetStatus) string {
	var message string
	if s.Ready {
		message = fmt.Sprintf("ValkeyCluster %q is ready (%d/%d shards)", targetName, s.ReadyCount, s.ObservedCount)
	} else {
		message = fmt.Sprintf("Waiting for ValkeyCluster %q (state: %s)", targetName, s.Phase)
	}

	condition := metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "ClusterNotReady",
		Message:            message,
		ObservedGeneration: cr.Generation,
	}
	if s.Ready {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ClusterReady"
	}
	meta.SetStatusCondition(&cr.Status.Conditions, condition)

	cr.Status.Ready = s.Ready
	cr.Status.Phase = s.Phase
	cr.Status.Shards = s.ObservedCount
	cr.Status.ReadyShards = s.ReadyCount
	cr.Status.Message = message

	return message
}
