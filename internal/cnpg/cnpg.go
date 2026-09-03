// Package cnpg contains everything that talks to CloudNativePG's
// postgresql.cnpg.io/v1 Cluster.
//
// We deliberately do not vendor CNPG's own Go API types or its CRD schema.
// Cluster is addressed purely through controller-runtime's dynamic client
// (unstructured.Unstructured), and the desired object is built as a plain
// map applied via Server-Side Apply. This keeps the operator decoupled from
// any specific CNPG version — as long as the postgresql.cnpg.io/v1 Cluster
// shape stays backward compatible, this operator keeps working without a
// rebuild.
//
// Adapter implements internal/reconciler's Adapter interface, so the actual
// reconciliation loop (finalizers, SSA, status-mirroring) lives once in
// internal/reconciler and is shared with every other vendor integration.
package cnpg

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
	"github.com/bartvanbenthem/paas-operator/internal/reconciler"
)

const (
	Group        = "postgresql.cnpg.io"
	Version      = "v1"
	Kind         = "Cluster"
	FieldManager = "postgrescluster-operator"
)

// GVK is the GroupVersionKind of the CNPG Cluster this operator manages.
var GVK = schema.GroupVersionKind{Group: Group, Version: Version, Kind: Kind}

// Adapter drives a PostgresCluster onto a same-named CNPG Cluster. It
// implements reconciler.Adapter[paasv1alpha1.PostgresCluster,
// *paasv1alpha1.PostgresCluster].
type Adapter struct{}

func (Adapter) GVK() schema.GroupVersionKind { return GVK }

// TargetName returns the name of the CNPG Cluster generated for crName. One
// PostgresCluster maps to exactly one same-named Cluster — there's no need
// for a separate naming scheme.
func (Adapter) TargetName(crName string) string { return crName }

func (Adapter) ObjectKind() string   { return "CNPG Cluster" }
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

// BuildManifest builds the desired postgresql.cnpg.io/v1 Cluster object for
// cr, ready to be applied via Server-Side Apply.
func (Adapter) BuildManifest(cr *paasv1alpha1.PostgresCluster, name, namespace, ownerName string) *unstructured.Unstructured {
	spec := cr.Spec

	storage := map[string]any{"size": spec.Storage.Size}
	if spec.Storage.StorageClass != "" {
		storage["storageClass"] = spec.Storage.StorageClass
	}

	clusterSpec := map[string]any{
		"instances": int64(spec.Instances),
		"storage":   storage,
		"bootstrap": map[string]any{
			"initdb": map[string]any{
				"database": spec.Database.Name,
				"owner":    spec.Database.Owner,
			},
		},
	}

	if spec.Image != "" {
		clusterSpec["imageName"] = spec.Image
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
		"app.kubernetes.io/managed-by": "postgrescluster-operator",
		"paas.example.com/owner":       ownerName,
	})
	u.Object["spec"] = clusterSpec

	return u
}

// ExtractStatus pulls phase/instances/readyInstances out of a CNPG
// Cluster's .status. ready is derived from instance counts rather than
// CNPG's free-form phase string, since the exact wording of phase is not a
// stable API contract across CNPG versions.
func (Adapter) ExtractStatus(u *unstructured.Unstructured) reconciler.TargetStatus {
	if u == nil {
		return reconciler.TargetStatus{Phase: "Unknown"}
	}

	phase, found, _ := unstructured.NestedString(u.Object, "status", "phase")
	if !found || phase == "" {
		phase = "Unknown"
	}
	i, _, _ := unstructured.NestedInt64(u.Object, "status", "instances")
	ri, _, _ := unstructured.NestedInt64(u.Object, "status", "readyInstances")
	instances := int32(i)
	readyInstances := int32(ri)
	ready := instances > 0 && readyInstances >= instances

	return reconciler.TargetStatus{
		Phase:         phase,
		Ready:         ready,
		ObservedCount: instances,
		ReadyCount:    readyInstances,
	}
}

// ApplyStatus mirrors s onto cr's own .status (instances/readyInstances +
// a standard Ready condition).
func (Adapter) ApplyStatus(cr *paasv1alpha1.PostgresCluster, targetName string, s reconciler.TargetStatus) string {
	var message string
	if s.Ready {
		message = fmt.Sprintf("CNPG Cluster %q is ready (%d/%d instances)", targetName, s.ReadyCount, s.ObservedCount)
	} else {
		message = fmt.Sprintf("Waiting for CNPG Cluster %q (phase: %s)", targetName, s.Phase)
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
	cr.Status.Instances = s.ObservedCount
	cr.Status.ReadyInstances = s.ReadyCount
	cr.Status.Message = message

	return message
}

// GetCluster fetches the current CNPG Cluster, returning (nil, nil) if it
// does not exist. A thin convenience wrapper over reconciler.GetTarget, kept
// here so callers (mainly tests) don't need to know the target's GVK.
func GetCluster(ctx context.Context, c client.Client, namespace, name string) (*unstructured.Unstructured, error) {
	return reconciler.GetTarget(ctx, c, GVK, namespace, name)
}
