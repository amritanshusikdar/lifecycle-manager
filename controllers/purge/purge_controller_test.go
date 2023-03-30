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
		var certRequest *unstructured.Unstructured

		By("Create the Kyma object", func() {
			Expect(controlPlaneClient.Create(ctx, kyma)).Should(Succeed())
			updateRequired := kyma.CheckLabelsAndFinalizers()
			if updateRequired {
				err := controlPlaneClient.Update(ctx, kyma)
				Expect(err).ToNot(HaveOccurred())
			}
		})

		By("Create some CR with finalizer(s)", func() {
			certRequest = createIssuerFor(kyma)
			Expect(controlPlaneClient.Create(ctx, certRequest)).Should(Succeed())
			Expect(getObjFinalizers(ctx, client.ObjectKeyFromObject(certRequest), controlPlaneClient)).Should(ContainElement(testFinalizer))
		})

		By("Kyma is deleted", func() {
			//Kyma delete event
			err := controlPlaneClient.Delete(ctx, kyma)
			Expect(err).ToNot(HaveOccurred())

			//Simulate main control loop action
			err = controlPlaneClient.Get(ctx, client.ObjectKeyFromObject(kyma), kyma)
			Expect(err).ToNot(HaveOccurred())

			err = purgeReconciler.UpdateStatus(ctx, kyma, v1beta1.StateDeleting, "TODO: Debugging")
			Expect(err).ToNot(HaveOccurred())

		})

		By("Target finalizers should be dropped", func() {
			Eventually(IsKymaInState(ctx, controlPlaneClient, kyma.GetName(), v1beta1.StateDeleting),
				Timeout, Interval).Should(BeTrue())
			Eventually(getObjFinalizers(ctx, client.ObjectKeyFromObject(certRequest), controlPlaneClient), Timeout, Interval).Should(BeEmpty())
		})

	})
})

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
