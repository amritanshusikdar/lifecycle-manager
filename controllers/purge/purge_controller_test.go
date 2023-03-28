package purge_test

import (
	"github.com/kyma-project/lifecycle-manager/api/v1beta1"
	. "github.com/kyma-project/lifecycle-manager/pkg/testutils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Kyma with no ModuleTemplate", Ordered, func() {
	kyma := NewTestKyma("no-module-kyma")
	RegisterDefaultLifecycleForKyma(kyma)

	It("Should result in a ready state immediately", func() {
		By("having transitioned the CR State to Ready as there are no modules")
		err := purgeReconciler.UpdateStatus(ctx, kyma, v1beta1.StateDeleting, "TODO: Debugging")
		Expect(err).ToNot(HaveOccurred())
		Eventually(IsKymaInState(ctx, controlPlaneClient, kyma.GetName(), v1beta1.StateDeleting),
			Timeout, Interval).Should(BeTrue())
	})
})
