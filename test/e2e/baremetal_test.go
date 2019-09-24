package e2e

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configapiv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"
)

func TestBaremetalDefaults(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	infrastructureConfig, err := client.Infrastructures().Get("cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if infrastructureConfig.Status.PlatformStatus.Type != configapiv1.BareMetalPlatformType {
		t.Skip("skipping on non-BareMetal platform")
	}

	// Start of the meaningful part
	defer framework.MustRemoveImageRegistry(t, client)
	framework.MustDeployImageRegistry(t, client, nil)
	cr := framework.MustEnsureImageRegistryIsProcessed(t, client)

	clusterOperator := framework.MustEnsureClusterOperatorStatusIsSet(t, client)
	for _, cond := range clusterOperator.Status.Conditions {
		switch cond.Type {
		case configapiv1.OperatorAvailable:
			if cond.Status != configapiv1.ConditionTrue {
				t.Errorf("expected clusteroperator to report Available=%s, got %s", configapiv1.ConditionTrue, cond.Status)
			}
			if cond.Reason != "Removed" {
				t.Errorf("expected reason to be 'Removed', got %s", cond.Reason)
			}
		case configapiv1.OperatorDegraded:
			if cond.Status != configapiv1.ConditionFalse {
				t.Errorf("the operator is not expected to be degraded, got: %s", cond.Status)
			}
		}
	}
}
