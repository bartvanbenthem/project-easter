// Package rabbitmq contains everything that talks to the RabbitMQ Cluster
// Operator's rabbitmq.com/v1beta1 RabbitmqCluster
// (github.com/rabbitmq/cluster-operator).
//
// As with internal/cnpg, internal/valkey, internal/grafana, and
// internal/mariadb, we deliberately do not vendor the RabbitMQ Cluster
// Operator's own Go API types or its CRD schema. RabbitmqCluster is
// addressed purely through controller-runtime's dynamic client
// (unstructured.Unstructured), and the desired object is built as a plain
// map applied via Server-Side Apply. This keeps the operator decoupled from
// any specific RabbitMQ Cluster Operator version. See
// crd-rabbitmq-cluster-operator-v2.22.5.yaml at the repo root for the schema
// this was built against.
//
// Adapter implements internal/reconciler's Adapter interface, so the actual
// reconciliation loop (finalizers, SSA, status-mirroring) lives once in
// internal/reconciler and is shared with every other vendor integration.
package rabbitmq

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	paasv1alpha1 "github.com/bartvanbenthem/paas-operator/api/v1alpha1"
	"github.com/bartvanbenthem/paas-operator/internal/ingress"
	"github.com/bartvanbenthem/paas-operator/internal/reconciler"
)

const (
	Group        = "rabbitmq.com"
	Version      = "v1beta1"
	Kind         = "RabbitmqCluster"
	FieldManager = "rabbitmqcluster-operator"

	// conditionTypeAllReplicasReady is the RabbitMQ Cluster Operator's own
	// condition type that goes True once every node in the StatefulSet is
	// ready. See internal/status/all_replicas_ready.go in
	// github.com/rabbitmq/cluster-operator.
	conditionTypeAllReplicasReady = "AllReplicasReady"

	// managementPort is the RabbitmqCluster's management UI port. The
	// RabbitMQ Cluster Operator always creates a Service named exactly like
	// the RabbitmqCluster itself, exposing this port -- a documented public
	// contract (https://www.rabbitmq.com/kubernetes/operator/using-operator),
	// not something this operator guesses at.
	managementPort = 15672
)

// GVK is the GroupVersionKind of the RabbitMQ Cluster Operator's
// RabbitmqCluster this operator manages.
var GVK = schema.GroupVersionKind{Group: Group, Version: Version, Kind: Kind}

// Adapter drives a paas RabbitMQCluster onto a same-named RabbitMQ Cluster
// Operator RabbitmqCluster. It implements
// reconciler.Adapter[paasv1alpha1.RabbitMQCluster, *paasv1alpha1.RabbitMQCluster].
type Adapter struct{}

func (Adapter) GVK() schema.GroupVersionKind { return GVK }

// TargetName returns the name of the RabbitmqCluster generated for crName.
// One paas RabbitMQCluster maps to exactly one same-named upstream
// RabbitmqCluster.
func (Adapter) TargetName(crName string) string { return crName }

func (Adapter) ObjectKind() string   { return "RabbitmqCluster" }
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

// BuildManifest builds the desired rabbitmq.com/v1beta1 RabbitmqCluster
// object for cr, ready to be applied via Server-Side Apply.
func (Adapter) BuildManifest(cr *paasv1alpha1.RabbitMQCluster, name, namespace, ownerName string) *unstructured.Unstructured {
	spec := cr.Spec

	persistence := map[string]any{"storage": spec.Storage.Size}
	if spec.Storage.StorageClass != "" {
		persistence["storageClassName"] = spec.Storage.StorageClass
	}

	clusterSpec := map[string]any{
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

// ExtraResources builds the Ingress fronting the RabbitmqCluster's
// management UI, when requested. Implements
// reconciler.ExtraResourcesAdapter[paasv1alpha1.RabbitMQCluster, *paasv1alpha1.RabbitMQCluster].
func (Adapter) ExtraResources(cr *paasv1alpha1.RabbitMQCluster, targetName, namespace, owner string) []reconciler.ExtraResource {
	name := targetName + "-ingress"

	var desired *unstructured.Unstructured
	if cr.Spec.Ingress != nil {
		desired = ingress.Build(cr.Spec.Ingress, name, namespace, owner, FieldManager, targetName, managementPort)
	}

	return []reconciler.ExtraResource{
		{GVK: ingress.GVK, Name: name, Desired: desired},
	}
}

// ExtractStatus pulls replicas and the "AllReplicasReady" condition out of a
// RabbitmqCluster's .status/.spec. The RabbitMQ Cluster Operator reports
// readiness solely via a set of standard metav1.Conditions -- there's no
// separate phase or ready-count field on its status -- so Phase is the
// "AllReplicasReady" condition's Reason and readyCount is derived: replicas
// (read back from the applied .spec, since status carries no copy of its
// own) when Ready is True, 0 otherwise.
func (Adapter) ExtractStatus(u *unstructured.Unstructured) reconciler.TargetStatus {
	if u == nil {
		return reconciler.TargetStatus{Phase: "Unknown"}
	}

	replicas, _, _ := unstructured.NestedInt64(u.Object, "spec", "replicas")
	observed := int32(replicas)

	conditions, _, _ := unstructured.NestedSlice(u.Object, "status", "conditions")
	phase := "Unknown"
	ready := false
	for _, c := range conditions {
		cond, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := cond["type"].(string); t != conditionTypeAllReplicasReady {
			continue
		}
		if s, _ := cond["status"].(string); s == "True" {
			ready = true
		}
		if r, _ := cond["reason"].(string); r != "" {
			phase = r
		}
		break
	}

	readyCount := int32(0)
	if ready {
		readyCount = observed
	}

	return reconciler.TargetStatus{
		Phase:         phase,
		Ready:         ready && observed > 0,
		ObservedCount: observed,
		ReadyCount:    readyCount,
	}
}

// ApplyStatus mirrors s onto cr's own .status (replicas/readyReplicas + a
// standard Ready condition).
func (Adapter) ApplyStatus(cr *paasv1alpha1.RabbitMQCluster, targetName string, s reconciler.TargetStatus) string {
	var message string
	if s.Ready {
		message = fmt.Sprintf("RabbitmqCluster %q is ready (%d/%d replicas)", targetName, s.ReadyCount, s.ObservedCount)
	} else {
		message = fmt.Sprintf("Waiting for RabbitmqCluster %q (phase: %s)", targetName, s.Phase)
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
	cr.Status.Replicas = s.ObservedCount
	cr.Status.ReadyReplicas = s.ReadyCount
	cr.Status.Message = message

	return message
}
