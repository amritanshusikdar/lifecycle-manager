package purge_test

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kyma-project/lifecycle-manager/api/v1beta1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func RegisterDefaultLifecycleForKyma(kyma *v1beta1.Kyma) {
	BeforeAll(func() {
		Expect(controlPlaneClient.Create(ctx, kyma)).Should(Succeed())
	})

	AfterAll(func() {
		Expect(controlPlaneClient.Delete(ctx, kyma)).Should(Succeed())
	})

	BeforeEach(func() {
		By("get latest kyma CR")
		Expect(controlPlaneClient.Get(ctx, client.ObjectKey{
			Name:      kyma.Name,
			Namespace: metav1.NamespaceDefault,
		}, kyma)).Should(Succeed())
	})
}
