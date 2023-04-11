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
	"time"

	"github.com/go-logr/logr"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kyma-project/lifecycle-manager/pkg/log"
	"github.com/kyma-project/lifecycle-manager/pkg/status"
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

const (
	KymaSpecGroup = "operator.kyma-project.io"
	KymaKind      = "Kyma"
)

type RemoteClientResolver func(context.Context, client.ObjectKey) (client.Client, error)

// PurgeReconciler reconciles a Kyma object.
type PurgeReconciler struct {
	client.Client
	record.EventRecorder
	RequeueIntervals
	signature.VerificationSettings
	SKRWebhookManager     watcher.SKRWebhookManager
	KcpRestConfig         *rest.Config
	RemoteClientCache     *remote.ClientCache
	ResolveRemoteClient   RemoteClientResolver
	PurgeFinalizerTimeout time.Duration
}

func (r *PurgeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := ctrlLog.FromContext(ctx)
	logger.V(log.InfoLevel).Info("Purge Reconciliation started")

	ctx = adapter.ContextWithRecorder(ctx, r.EventRecorder)

	// check if kyma resource exists
	kyma := &v1beta1.Kyma{}
	if err := r.Get(ctx, req.NamespacedName, kyma); err != nil { //nolint:nestif
		if apierrors.IsNotFound(err) {
			remoteClient, err := r.ResolveRemoteClient(ctx, client.ObjectKey{Namespace: req.Namespace, Name: req.Name})
			if err != nil {
				return ctrl.Result{}, err
			}
			if done, msg := performCleanup(ctx, remoteClient, logger); done {
				logger.Info(fmt.Sprintf("%s", msg))
			} else {
				logger.Info("Purging was not successful!")
				logger.Info(fmt.Sprintf("%s", msg))
				// TODO: Should we requeue? If the error is permanent, we may end up in the endless loop...
			}

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, err
	}

	// condition to check if deletionTimestamp is set, retry until it gets fully deleted
	if !kyma.DeletionTimestamp.IsZero() { //nolint:nestif
		deletionDeadline := kyma.DeletionTimestamp.Add(r.PurgeFinalizerTimeout)

		if time.Now().After(deletionDeadline) {
			remoteClient, err := r.ResolveRemoteClient(ctx, client.ObjectKeyFromObject(kyma))
			if err != nil {
				return ctrl.Result{}, err
			}
			if done, msg := performCleanup(ctx, remoteClient, logger); done {
				logger.Info(fmt.Sprintf("%s", msg))
			} else {
				logger.Info("Purging was not successful!")
				logger.Info(fmt.Sprintf("%s", msg))
			}
			return ctrl.Result{}, nil
		}

		return ctrl.Result{
			RequeueAfter: time.Until(deletionDeadline.Add(time.Second)),
		}, nil
	}

	return ctrl.Result{}, nil
}

func performCleanup(ctx context.Context, remoteClient client.Client, logger logr.Logger) (bool, interface{}) {
	crdList := apiextensions.CustomResourceDefinitionList{}

	if err := remoteClient.List(ctx, &crdList); err != nil {
		return false, err
	}

	for _, crdResource := range crdList.Items {
		if isKymaCR(crdResource) {
			continue
		}
		staleResources, err := getStaleResourcesFrom(ctx, remoteClient, crdResource)
		if err != nil {
			return false, err
		}

		pending, err := purgeStaleResources(ctx, remoteClient, staleResources)
		if pending > 0 {
			logger.Info(fmt.Sprintf("Still %d resources pending to purge from %s", pending, crdResource.GetName()))
		}
		if err != nil {
			return false, err
		}
	}
	return true, "Purge of stale resources successful!"
}

func getStaleResourcesFrom(ctx context.Context, remoteClient client.Client,
	crd apiextensions.CustomResourceDefinition,
) (unstructured.UnstructuredList, error) {
	staleResources := unstructured.UnstructuredList{}
	//	Since there are multiple possible versions, we are choosing the one that's in the etcd storage
	var gvkVersion string
	for _, version := range crd.Spec.Versions {
		if version.Storage {
			gvkVersion = version.Name
			break
		}
	}

	gvk := schema.GroupVersionKind{
		Group:   crd.Spec.Group,
		Kind:    crd.Spec.Names.Kind,
		Version: gvkVersion,
	}

	staleResources.SetGroupVersionKind(gvk)

	if err := remoteClient.List(ctx, &staleResources); err != nil {
		return unstructured.UnstructuredList{}, err
	}

	return staleResources, nil
}

func purgeStaleResources(ctx context.Context, remoteClient client.Client,
	staleResources unstructured.UnstructuredList,
) (int, error) {
	logger := ctrlLog.FromContext(ctx)

	pending := 0
	for index := range staleResources.Items {
		resource := staleResources.Items[index]
		for _, finalizer := range resource.GetFinalizers() {
			if removed := controllerutil.RemoveFinalizer(&resource, finalizer); !removed {
				logger.V(log.WarnLevel).Info(fmt.Sprintf("Could not purge finalizer `%s` from resource `%s`",
					finalizer, resource.GetName()))
				pending++
			} else {
				logger.Info(fmt.Sprintf("Successfully purged finalizer `%s` from resource `%s`",
					finalizer, resource.GetName()))
			}
		}
		if err := remoteClient.Update(ctx, &resource); err != nil {
			return pending, err
		}
	}
	return pending, nil
}

func (r *PurgeReconciler) UpdateStatus(
	ctx context.Context, kyma *v1beta1.Kyma, state v1beta1.State, message string,
) error {
	if err := status.Helper(r).UpdateStatusForExistingModules(ctx, kyma, state, message); err != nil {
		return fmt.Errorf("error while updating status to %s because of %s: %w", state, message, err)
	}
	return nil
}

func (r *PurgeReconciler) RecordKymaStatusMetrics(ctx context.Context, kyma *v1beta1.Kyma) {
	_ = ctx
	_ = kyma
}

func (r *PurgeReconciler) IsKymaManaged() bool { return false }

func isKymaCR(crd apiextensions.CustomResourceDefinition) bool {
	if crd.Spec.Group == KymaSpecGroup && crd.Spec.Names.Kind == KymaKind {
		return true
	}
	return false
}
