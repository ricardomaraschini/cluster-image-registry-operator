package e2e

import (
	"testing"

	"github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1"
	"github.com/openshift/cluster-image-registry-operator/test/framework"

	configapiv1 "github.com/openshift/api/config/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestBaremetalDefaults(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	infra, err := client.Infrastructures().Get("cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.BareMetalPlatformType {
		t.Skip("skipping on non-BareMetal platform")
	}

	// Start of the meaningful part
	defer framework.MustRemoveImageRegistry(t, client)

	framework.MustDeployImageRegistry(t, client, nil)
	cr := framework.MustEnsureImageRegistryIsProcessed(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)

	conds := framework.GetImageRegistryConditions(cr)
	if conds.Available.Reason() != "Removed" {
		t.Errorf("exp Available reason: Removed, got %s", conds.Available.Reason())
	}
	if conds.Degraded.Reason() != "Removed" {
		t.Errorf("exp Degraded reason: Removed, got %s", conds.Degraded.Reason())
	}
	if conds.Progressing.Reason() != "Removed" {
		t.Errorf("exp Progressing reason: Removed, got %s", conds.Progressing.Reason())
	}
}

func TestBaremetalDay2Operations(t *testing.T) {
	client := framework.MustNewClientset(t, nil)

	infra, err := client.Infrastructures().Get("cluster", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if infra.Status.PlatformStatus.Type != configapiv1.BareMetalPlatformType {
		t.Skip("skipping on non-BareMetal platform")
	}

	// Start of the meaningful part
	defer framework.MustRemoveImageRegistry(t, client)

	framework.MustDeployImageRegistry(t, client, nil)
	cf := framework.MustEnsureImageRegistryIsProcessed(t, client)

	// Set registry to managed with empty dir storage engine.
	cf.Spec.ManagementState = operatorapi.Managed
	cf.Spec.Storage.EmptyDir = &v1.ImageRegistryConfigStorageEmptyDir{}
	if _, err := client.Configs().Update(cf); err != nil {
		t.Errorf("error updating config: %v", err)
	}

	framework.MustEnsureOperatorIsNotHotLooping(t, client)
	framework.MustEnsureImageRegistryIsAvailable(t, client)
	framework.MustEnsureInternalRegistryHostnameIsSet(t, client)
	framework.MustEnsureClusterOperatorStatusIsNormal(t, client)
}
