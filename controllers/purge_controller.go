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
	fmt.Println("Purge Reconciler loop runs")

	ctx = adapter.ContextWithRecorder(ctx, r.EventRecorder)

	// check if kyma resource exists
	kyma := &v1beta1.Kyma{}
	if err := r.Get(ctx, req.NamespacedName, kyma); err != nil {
		// we'll ignore not-found errors, since they can't be fixed by an immediate
		// requeue (we'll need to wait for a new notification), and we can get them
		// on deleted requests.
		logger.Info("Deleted successfully!")

		return ctrl.Result{}, client.IgnoreNotFound(err) //nolint:wrapcheck
	}

	// check if deletionTimestamp is set, retry until it gets fully deleted
	if !kyma.DeletionTimestamp.IsZero() && kyma.Status.State == v1beta1.StateDeleting {

		//Check if enough time has passed

		//About the times:
		//1) DeletionTimestamp - set by K8s
		//2) Then after some time our controller is notified
		//3) Then after some time our controller changes the Kyma status to "Deleting"
		// If time difference between 1 and 3 is about 1 second or less, we may use "DeletionTimestamp" directly.
		// If it's longer then maybe we should base our logic on the time when 3) happened.

		timeRequired := kyma.DeletionTimestamp.Add(r.PurgeFinalizerTimeout)

		fmt.Println("r.PurgeFinalizerTimeout", r.PurgeFinalizerTimeout)
		fmt.Println("timeRequired", timeRequired)
		fmt.Println("time.Now()", time.Now())

		if time.Now().After(timeRequired) {
			//In this block of code the actual finalizer dropping on the target cluster should happen.
			fmt.Println("Deleting finalizers...")

			//	Temporary means to iterate through all the resources
			var crdList = apiextensions.CustomResourceDefinitionList{}

			err := r.Client.List(ctx, &crdList)
			if err != nil {
				return ctrl.Result{}, err
			}

			for _, crdResource := range crdList.Items {
				var gvkVersion string
				for _, version := range crdResource.Spec.Versions {
					if version.Storage {
						gvkVersion = version.Name
						break
					}
				}

				//for every CRD
				//1) Get the Kind, Group, Version of the type the CRD describes and create GVK
				gvk := schema.GroupVersionKind{Group: crdResource.Spec.Group,
					Kind:    crdResource.Spec.Names.Kind,
					Version: gvkVersion,
				}
				fmt.Println("GVK: ", gvk)
				//2) Somehow use r.Client.List(...) to get all the objects of the GVK
				unstructuredList := unstructured.UnstructuredList{}
				unstructuredList.SetGroupVersionKind(gvk)

				err = r.List(ctx, &unstructuredList)
				if err != nil {
					return ctrl.Result{}, err
				}

				for innerIndex, resource := range unstructuredList.Items {
					fmt.Println("\t", innerIndex, ": ", resource.GetName(), resource.GetNamespace())
					for _, finalizer := range resource.GetFinalizers() {
						done := controllerutil.RemoveFinalizer(&resource, finalizer)
						fmt.Println("FINALIZER REMOVED? --> ", done)
						if err := r.Update(ctx, &resource); err != nil {
							return ctrl.Result{}, err
						}
					}
				}
			}
			/*
				TODO:
					cleanup the codebase
					do proper error handling
					uncomment the previously for testing purposes commented line
					shift to different file
					split each functionality into smaller helper functions
			*/
			return ctrl.Result{}, nil
		}

		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: 1 * time.Second,
		}, nil
	}

	return ctrl.Result{}, nil
}
