package purge_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kyma-project/lifecycle-manager/api/v1beta1"
	. "github.com/kyma-project/lifecycle-manager/pkg/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrSpecDataMismatch          = errors.New("spec.data not match")
	ErrStatusModuleStateMismatch = errors.New("status.modules.state not match")
)

var _ = Describe("A Kyma in the deleting state", Ordered, func() {

	It("should trigger purge reconcile loop", func() {
		kyma := NewTestKyma("no-module-kyma")
		//Pretend the main controller is deleting Kyma
		kyma.CheckLabelsAndFinalizers()

		Expect(controlPlaneClient.Create(ctx, kyma)).Should(Succeed())
		time.Sleep(2 * time.Millisecond)

		fmt.Fprint(GinkgoWriter, "\n")
		fmt.Fprint(GinkgoWriter, "--------------------------------------------------------------------------------")
		fmt.Fprint(GinkgoWriter, "\n")

		time.Sleep(2 * time.Millisecond)
		//controlPlaneClient.Status().Update(ctx, kyma, )
		err := controlPlaneClient.Status().Patch(ctx, kyma, client.Apply, subResourceOpts(client.ForceOwnership),
			client.FieldOwner(v1beta1.OperatorName))
		//err := purgeReconciler.UpdateStatus(ctx, kyma, v1beta1.StateDeleting, "Testing")
		Expect(err).ToNot(HaveOccurred())

		time.Sleep(10 * time.Millisecond)
		kyma2 := v1beta1.Kyma{}
		err = controlPlaneClient.Get(ctx, client.ObjectKeyFromObject(kyma), &kyma2)

		fmt.Fprint(GinkgoWriter, "\n\nkyma2.Status:", kyma2.Status)
		fmt.Fprint(GinkgoWriter, "\n\nkyma2.Finalizers:", kyma2.Finalizers)

		fmt.Fprint(GinkgoWriter, "\n")
		fmt.Fprint(GinkgoWriter, "^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^")
		fmt.Fprint(GinkgoWriter, "\n")
		Expect(err).ToNot(HaveOccurred())

		controlPlaneClient.Delete(ctx, kyma)
		Eventually(IsKymaInState(ctx, controlPlaneClient, kyma.GetName(), v1beta1.StateReady),
			/*Timeout*/ time.Second*3, Interval).Should(BeTrue())
	})
})

func CheckKymaConditions(ctx context.Context, kcpClient client.Client, kymaName string,
	requiredConditions []v1beta1.KymaConditionType) func() bool {
	return func() bool {
		kymaFromCluster, err := GetKyma(ctx, kcpClient, kymaName, "")
		if err != nil || len(kymaFromCluster.Status.Conditions) != len(requiredConditions) {
			return false
		}

		for _, conditionType := range requiredConditions {
			exists := false
			for _, kymaCondition := range kymaFromCluster.Status.Conditions {
				if kymaCondition.Type == string(conditionType) {
					exists = true
					break
				}
			}
			if !exists {
				return false
			}
		}
		return true
	}
}

func subResourceOpts(opts ...client.PatchOption) client.SubResourcePatchOption {
	return &client.SubResourcePatchOptions{PatchOptions: *(&client.PatchOptions{}).ApplyOptions(opts)}
}
