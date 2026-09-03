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

// Package reconciler provides the reconciliation engine shared by every
// vendor integration in this operator (cnpg, valkey, ...). Each of our own
// paas CRDs maps 1:1 onto a same-named object of some foreign, vendor-owned
// GVK; an Adapter describes that mapping for one vendor, and
// GenericReconciler drives the shared finalizer-gated create/update/delete
// and status-mirroring loop against it. This is what lets each type be
// added/owned/versioned independently (its own CRD, its own adapter
// package) while the actual CRUD/reconciliation logic is written once.
package reconciler

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// FinalizerName is added to every paas CR reconciled through this package,
// gating removal of the CR on cleanup of its foreign target object.
const FinalizerName = "paas.example.com/cleanup"

// TargetStatus is the subset of a foreign target object's status that every
// Adapter extracts and every paas CR mirrors back onto itself.
type TargetStatus struct {
	// Phase is a verbatim copy of the target's own status.phase (or
	// equivalent), for display. "Unknown" if the target has none yet.
	Phase string
	// Ready is true once the target reports ObservedCount == ReadyCount
	// and ObservedCount > 0.
	Ready bool
	// ObservedCount and ReadyCount are the target's own notion of "how many
	// units make this up" and "how many of those are ready" -- CNPG's
	// instances/readyInstances, Valkey's shards/readyShards, etc.
	ObservedCount int32
	ReadyCount    int32
}

// ObjectPtr constrains PT to "pointer to T that implements client.Object" --
// the usual way to write a generic reconciler over concrete Kubernetes API
// types without reflection: T is the CR struct (e.g.
// paasv1alpha1.PostgresCluster), PT is *T.
type ObjectPtr[T any] interface {
	client.Object
	*T
}

// Adapter is everything specific to one vendor integration: the foreign
// target GVK a paas CR of type PT maps to, how to build and read that
// target, and how to mirror its status back onto the CR.
type Adapter[T any, PT ObjectPtr[T]] interface {
	// GVK is the GroupVersionKind of the foreign target object this
	// adapter's CR maps to.
	GVK() schema.GroupVersionKind
	// TargetName returns the name of the target object for a CR named
	// crName.
	TargetName(crName string) string
	// ObjectKind is a human-readable label for the target, used in log
	// messages and events (e.g. "CNPG Cluster").
	ObjectKind() string
	// FieldManager is used for the Server-Side Apply of the target object.
	FieldManager() string
	// BuildManifest builds the desired target object for cr.
	BuildManifest(cr PT, name, namespace, owner string) *unstructured.Unstructured
	// ExtractStatus reads the target object's observed status.
	ExtractStatus(u *unstructured.Unstructured) TargetStatus
	// ApplyStatus mirrors s onto cr's own .status (conditions included) and
	// returns a human-readable summary message.
	ApplyStatus(cr PT, targetName string, s TargetStatus) (message string)
}

// GenericReconciler drives any paas CR type PT towards a matching,
// same-named foreign target object (as described by Adapter) and mirrors
// that object's status back onto the CR. This is the reconciliation logic
// shared by every vendor integration: finalizer-gated cleanup, idempotent
// Server-Side Apply, status-mirroring, and event recording.
//
// No secondary watch is wired on the foreign target: it's typically a
// foreign, unstructured type not registered with the manager's scheme, so
// an Owns()-style watch isn't available the way it is for typed children.
// Status convergence is picked up by polling instead of a push trigger --
// fast while not ready, slow once settled.
type GenericReconciler[T any, PT ObjectPtr[T]] struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
	Adapter  Adapter[T, PT]
	// Name is the controller name passed to ctrl.Builder.Named().
	Name string
}

// Reconcile implements reconcile.Reconciler.
func (r *GenericReconciler[T, PT]) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var obj T
	cr := PT(&obj)
	if err := r.Get(ctx, req.NamespacedName, cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	targetName := r.Adapter.TargetName(cr.GetName())
	kind := r.Adapter.ObjectKind()
	log = log.WithValues("target", targetName)

	// -------------------------------------------------------------------
	// Deletion path
	// -------------------------------------------------------------------
	if !cr.GetDeletionTimestamp().IsZero() {
		if controllerutil.ContainsFinalizer(cr, FinalizerName) {
			log.Info(fmt.Sprintf("deletion timestamp set — deleting %s", kind))

			deleted, err := r.deleteTarget(ctx, cr.GetNamespace(), targetName)
			if err != nil {
				log.Error(err, fmt.Sprintf("failed to delete %s", kind))
				return ctrl.Result{}, err
			}
			if deleted {
				log.Info(fmt.Sprintf("deleted %s", kind))
			} else {
				log.Info(fmt.Sprintf("%s was already gone", kind))
			}

			controllerutil.RemoveFinalizer(cr, FinalizerName)
			if err := r.Update(ctx, cr); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("finalizer removed — deletion complete")
		}
		return ctrl.Result{}, nil
	}

	// -------------------------------------------------------------------
	// Normal reconcile path
	// -------------------------------------------------------------------

	// 1. Ensure finalizer is present so we get a chance to clean up the
	//    target before the CR itself is removed.
	if !controllerutil.ContainsFinalizer(cr, FinalizerName) {
		controllerutil.AddFinalizer(cr, FinalizerName)
		if err := r.Update(ctx, cr); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// 2. Detect create-vs-update up front, purely for event recording -- the
	//    apply below is idempotent either way.
	existing, err := r.getTarget(ctx, cr.GetNamespace(), targetName)
	if err != nil {
		return ctrl.Result{}, err
	}

	// 3. Apply the desired target object via Server-Side Apply.
	desired := r.Adapter.BuildManifest(cr, targetName, cr.GetNamespace(), cr.GetName())
	if err := r.applyTarget(ctx, desired); err != nil {
		log.Error(err, fmt.Sprintf("failed to apply %s", kind))
		return ctrl.Result{}, err
	}
	log.Info(fmt.Sprintf("applied %s", kind))

	if existing == nil {
		r.Recorder.Eventf(cr, nil, corev1.EventTypeNormal, "TargetCreated", "Sync",
			"%s %q created in namespace %q", kind, targetName, cr.GetNamespace())
	}

	// 4. Mirror the target's status onto our own CR.
	status := r.Adapter.ExtractStatus(desired)
	r.Adapter.ApplyStatus(cr, targetName, status)

	if err := r.Status().Update(ctx, cr); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconcile complete", "ready", status.Ready)

	requeueAfter := 15 * time.Second
	if status.Ready {
		requeueAfter = 5 * time.Minute
	}
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *GenericReconciler[T, PT]) SetupWithManager(mgr ctrl.Manager) error {
	var obj T
	return ctrl.NewControllerManagedBy(mgr).
		For(PT(&obj)).
		Named(r.Name).
		Complete(r)
}

func (r *GenericReconciler[T, PT]) getTarget(ctx context.Context, namespace, name string) (*unstructured.Unstructured, error) {
	return GetTarget(ctx, r.Client, r.Adapter.GVK(), namespace, name)
}

// applyTarget applies the desired target object via Server-Side Apply.
// desired is updated in place with the object as merged by the API server
// (including .status, if the vendor controller has already populated one).
func (r *GenericReconciler[T, PT]) applyTarget(ctx context.Context, desired *unstructured.Unstructured) error {
	// client.Client.Apply (the newer typed SSA method) takes a
	// runtime.ApplyConfiguration, which unstructured.Unstructured does not
	// implement -- there is no generated apply-configuration for a foreign
	// CRD we don't own. The Patch(client.Apply) form remains the correct
	// way to Server-Side-Apply an unstructured object.
	return r.Client.Patch(ctx, desired, client.Apply, client.FieldOwner(r.Adapter.FieldManager()), client.ForceOwnership) //nolint:staticcheck
}

func (r *GenericReconciler[T, PT]) deleteTarget(ctx context.Context, namespace, name string) (bool, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(r.Adapter.GVK())
	u.SetNamespace(namespace)
	u.SetName(name)
	err := r.Delete(ctx, u)
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetTarget fetches the current foreign target object identified by gvk,
// returning (nil, nil) if it does not exist. Exposed (in addition to being
// used internally) so tests can assert on the generated manifest without
// driving a full Reconcile.
func GetTarget(ctx context.Context, c client.Client, gvk schema.GroupVersionKind, namespace, name string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, u)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}
