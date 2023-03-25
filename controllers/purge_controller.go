/*
Copyright 2022.

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

package controllers

import (
	"context"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"time"

	"github.com/kyma-project/lifecycle-manager/pkg/log"
	"k8s.io/client-go/rest"

	"github.com/kyma-project/lifecycle-manager/api/v1beta1"
	"github.com/kyma-project/lifecycle-manager/pkg/adapter"
	"github.com/kyma-project/lifecycle-manager/pkg/remote"
	"github.com/kyma-project/lifecycle-manager/pkg/signature"
	"github.com/kyma-project/lifecycle-manager/pkg/watcher"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
)

// PurgeReconciler reconciles a Kyma object.
type PurgeReconciler struct {
	client.Client
	record.EventRecorder
	RequeueIntervals
	signature.VerificationSettings
	SKRWebhookManager     watcher.SKRWebhookManager
	KcpRestConfig         *rest.Config
	RemoteClientCache     *remote.ClientCache
	PurgeFinalizerTimeout time.Duration
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PurgeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrlLog.FromContext(ctx)
	logger.V(log.InfoLevel).Info("reconciling")

	ctx = adapter.ContextWithRecorder(ctx, r.EventRecorder)

	// check if kyma resource exists
	kyma := &v1beta1.Kyma{}
	if err := r.Get(ctx, req.NamespacedName, kyma); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		logger.Info("Deleted successfully!")

		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// condition to check if deletionTimestamp is set, retry until it gets fully deleted
	if !kyma.DeletionTimestamp.IsZero() && kyma.Status.State == v1beta1.StateDeleting {
		deletionDeadline := kyma.DeletionTimestamp.Add(r.PurgeFinalizerTimeout)

		if time.Now().After(deletionDeadline) {
			fmt.Println("Deleting finalizers...")

			var crdList = apiextensions.CustomResourceDefinitionList{}

			if err := r.Client.List(ctx, &crdList); err != nil {
				return ctrl.Result{}, err
			}

			for _, crdResource := range crdList.Items {
				staleResources, err := getStaleResourcesFrom(ctx, r, crdResource)
				if err != nil {
					return ctrl.Result{}, nil
				}

				pending, err := purgeStaleResources(ctx, r, staleResources)
				if pending > 0 {
					logger.Info(fmt.Sprintf("Still %d resources pending to purge", pending))
				}
				if err != nil {
					return ctrl.Result{}, err
				}
			}
			return ctrl.Result{}, nil
		}

		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: 1 * time.Second,
		}, nil
	}

	return ctrl.Result{}, nil
}

// Helper functions
func getStaleResourcesFrom(ctx context.Context, r *PurgeReconciler,
	crd apiextensions.CustomResourceDefinition) (staleResources unstructured.UnstructuredList, err error) {
	//	Since there are multiple possible versions, we are choosing the one that's in the etcd storage
	var gvkVersion string
	for _, version := range crd.Spec.Versions {
		if version.Storage {
			gvkVersion = version.Name
			break
		}
	}

	gvk := schema.GroupVersionKind{Group: crd.Spec.Group,
		Kind:    crd.Spec.Names.Kind,
		Version: gvkVersion,
	}

	staleResources.SetGroupVersionKind(gvk)

	if err := r.List(ctx, &staleResources); err != nil {
		return unstructured.UnstructuredList{}, err
	}

	return staleResources, nil
}

func purgeStaleResources(ctx context.Context, r *PurgeReconciler,
	staleResources unstructured.UnstructuredList) (pending int, err error) {
	logger := ctrlLog.FromContext(ctx)

	for _, resource := range staleResources.Items {
		for _, finalizer := range resource.GetFinalizers() {
			if removed := controllerutil.RemoveFinalizer(&resource, finalizer); !removed {
				logger.V(log.WarnLevel).Info(fmt.Sprintf("Could not purge finalizer `%s` from resource `%s`",
					finalizer, resource.GetName()))
				pending++
			} else {
				logger.Info(fmt.Sprintln(fmt.Sprintf("Successfully purged finalizer `%s` from resource `%s`",
					finalizer, resource.GetName())))
			}
		}
		if err := r.Update(ctx, &resource); err != nil {
			return pending, err
		}
	}
	return pending, nil
}
