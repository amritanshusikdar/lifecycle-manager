package purge_test

import (
	"context"

	"github.com/kyma-project/lifecycle-manager/api/v1beta1"
	. "github.com/kyma-project/lifecycle-manager/pkg/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	testFinalizer = "purge.reconciler/test"
)

var _ = Describe("When kyma is not deleted within configured timeout", Ordered, func() {
	kyma := NewTestKyma("no-module-kyma")

	It("The purge logic should start after the timeout", func() {
		var issuerCR *unstructured.Unstructured

		By("Create the Kyma object", func() {
			Expect(controlPlaneClient.Create(ctx, kyma)).Should(Succeed())
			updateRequired := kyma.CheckLabelsAndFinalizers()
			if updateRequired {
				err := controlPlaneClient.Update(ctx, kyma)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		By("Create some CR with finalizer(s)", func() {
			issuerCR = createIssuerFor(kyma)
			Expect(controlPlaneClient.Create(ctx, issuerCR)).Should(Succeed())
			Expect(getObjFinalizers(ctx, client.ObjectKeyFromObject(issuerCR), controlPlaneClient)).Should(ContainElement(testFinalizer))
		})

		By("Kyma deletion is triggered", func() {
			//Kyma delete event
			err := controlPlaneClient.Delete(ctx, kyma)
			Expect(err).ToNot(HaveOccurred())

			//Simulate main control loop action
			Eventually(updateKymaStatus(ctx, controlPlaneClient, purgeReconciler.UpdateStatus, client.ObjectKeyFromObject(kyma), v1beta1.StateDeleting), Timeout, Interval).
				Should(Succeed())

		})

		By("Target finalizers should be dropped", func() {
			Eventually(IsKymaInState(ctx, controlPlaneClient, kyma.GetName(), v1beta1.StateDeleting),
				Timeout, Interval).Should(BeTrue())
			Eventually(getObjFinalizers, 3*Timeout, Interval).
				WithContext(ctx).
				WithArguments(client.ObjectKeyFromObject(issuerCR), controlPlaneClient).
				Should(BeEmpty())
		})

	})
})

/*
var _ = Describe("When kyma is deleted before configured timeout", Ordered, func() {
		By("Target finalizers should be dropped as soon as possible", func() {
		})
})
*/

func createIssuerObj() *unstructured.Unstructured {
	gvk := schema.GroupVersionKind{
		Group:   "cert-manager.io",
		Version: "v1",
		Kind:    "Issuer",
	}
	res := unstructured.Unstructured{}
	res.SetGroupVersionKind(gvk)
	return &res
}

func createIssuerFor(kyma *v1beta1.Kyma) *unstructured.Unstructured {
	res := createIssuerObj()
	res.SetName(kyma.Name)
	res.SetNamespace(kyma.Namespace)
	res.SetFinalizers([]string{testFinalizer})

	unstructured.SetNestedMap(res.Object, map[string]interface{}{}, "spec")
	unstructured.SetNestedMap(res.Object, map[string]interface{}{}, "spec", "ca")
	unstructured.SetNestedField(res.Object, "foobar", "spec", "ca", "secretName")

	return res
}

func getObjFinalizers(ctx context.Context, key client.ObjectKey, cl client.Client) []string {
	res := createIssuerObj()
	Expect(cl.Get(ctx, key, res)).Should(Succeed())
	return res.GetFinalizers()
}

func updateKymaStatus(ctx context.Context, cl client.Client, updateStatus func(context.Context, *v1beta1.Kyma, v1beta1.State, string) error, key client.ObjectKey, state v1beta1.State) func() error {
	return func() error {
		kyma := v1beta1.Kyma{}

		err := cl.Get(ctx, key, &kyma)
		if err != nil {
			return err
		}

		err = updateStatus(ctx, &kyma, v1beta1.StateDeleting, "TODO: Debugging")
		if err != nil {
			return err
		}

		return nil
	}
}
