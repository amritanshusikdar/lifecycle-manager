package purge_test

import (
	"context"
	"errors"
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
	kyma := NewTestKyma("no-module-kyma")

	//Pretend the main controller is deleting Kyma
	kyma.CheckLabelsAndFinalizers()
	kyma.Status = v1beta1.KymaStatus{
		State: v1beta1.StateDeleting,
	}
	RegisterDefaultLifecycleForKyma(kyma)

	It("should trigger purge reconcile loop", func() {
		controlPlaneClient.Delete(ctx, kyma)
		Eventually(IsKymaInState(ctx, controlPlaneClient, kyma.GetName(), v1beta1.StateReady),
			/*Timeout*/ time.Second*3, Interval).Should(BeTrue())
	})
})

func expectKymaStatusModules(kymaName string, state v1beta1.State) func() error {
	return func() error {
		createdKyma, err := GetKyma(ctx, controlPlaneClient, kymaName, "")
		if err != nil {
			return err
		}
		for _, moduleStatus := range createdKyma.Status.Modules {
			if moduleStatus.State != state {
				return ErrStatusModuleStateMismatch
			}
		}
		return nil
	}
}

func updateAllModules(kymaName string, state v1beta1.State) func() error {
	return func() error {
		createdKyma, err := GetKyma(ctx, controlPlaneClient, kymaName, "")
		if err != nil {
			return err
		}
		for _, activeModule := range createdKyma.Spec.Modules {
			if updateModuleState(createdKyma, activeModule, state) != nil {
				return err
			}
		}
		return nil
	}
}

func updateModuleTemplateSpecData(kymaName, valueUpdated string) func() error {
	return func() error {
		createdKyma, err := GetKyma(ctx, controlPlaneClient, kymaName, "")
		if err != nil {
			return err
		}
		for _, activeModule := range createdKyma.Spec.Modules {
			moduleTemplate, err := GetModuleTemplate(activeModule.Name)
			Expect(err).ToNot(HaveOccurred())
			moduleTemplate.Spec.Data.Object["spec"] = map[string]any{"initKey": valueUpdated}
			err = controlPlaneClient.Update(ctx, moduleTemplate)
			Expect(err).ToNot(HaveOccurred())
		}
		return nil
	}
}

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
