package operator

import (
	"bytes"
	"fmt"
	"time"

	mcfgv1 "github.com/openshift/machine-config-operator/pkg/apis/machineconfiguration.openshift.io/v1"
	informerv1 "github.com/openshift/machine-config-operator/pkg/generated/informers/externalversions/machineconfiguration.openshift.io/v1"
	listerv1 "github.com/openshift/machine-config-operator/pkg/generated/listers/machineconfiguration.openshift.io/v1"
	"github.com/vincent-petithory/dataurl"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	setv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

// NewNodePullSecretsController returns a controller that keep track of docker
// pull secrets in used by the cluster nodes.
func NewNodePullSecretsController(
	pools informerv1.MachineConfigPoolInformer,
	mcfgLister listerv1.MachineConfigLister,
	secretsLister setv1.SecretInterface,
) *NodePullSecretsController {
	p := &NodePullSecretsController{
		secretsLister: secretsLister,
		mcfgLister:    mcfgLister,
		pools:         pools,
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"NodePullSecretsController",
		),
	}
	pools.Informer().AddEventHandler(p)
	return p
}

// NodePullSecretsController copis all node's pull secrets into a kubernetes
// secret within image registry's namespace. Watches 'worker' pool nodes and
// updates the secret every time the pool configuration changes.
type NodePullSecretsController struct {
	secretsLister setv1.SecretInterface
	mcfgLister    listerv1.MachineConfigLister
	pools         informerv1.MachineConfigPoolInformer
	queue         workqueue.RateLimitingInterface
}

// currentMachineConfig returns the current MachineConfig name in use by
// the worker pool nodes.
func (n *NodePullSecretsController) currentMachineConfig() (string, error) {
	pool, err := n.pools.Lister().Get("worker")
	if err != nil {
		return "", err
	}

	if len(pool.Spec.Configuration.Name) == 0 {
		return "", fmt.Errorf("no rendered MachineConfig name found")
	}

	klog.Infof(
		"worker's machineconfigpool config using %q",
		pool.Spec.Configuration.Name,
	)

	return pool.Spec.Configuration.Name, nil
}

// sync is called every time an update happens on a machine config pool.
func (n *NodePullSecretsController) sync() error {
	sec, notfound, err := n.secret()
	if err != nil {
		return err
	}

	mcfgName, err := n.currentMachineConfig()
	if err != nil {
		return err
	}

	mc, err := n.mcfgLister.Get(mcfgName)
	if err != nil {
		return err
	}

	// content is still the same, nothing else to do.
	if !n.updateSecret(sec, mc) {
		return nil
	}

	// XXX can't we "upsert" ?
	if notfound {
		_, err = n.secretsLister.Create(sec)
	} else {
		_, err = n.secretsLister.Update(sec)
	}
	return err
}

// loop keeps reading events from the workqueue and dispatches sync every time.
func (n *NodePullSecretsController) loop() {
	for {
		obj, shutdown := n.queue.Get()
		if shutdown {
			return
		}

		if err := n.sync(); err != nil {
			klog.Errorf("unable to sync pull secrets: %v", err)
			n.queue.AddRateLimited(obj)
		} else {
			klog.Info("node pull secrets' sync finished.")
			n.queue.Forget(obj)
		}

		n.queue.Done(obj)
	}
}

// secret returns the namespace secret or a new one if not present.
//
// Returns the secret, if it is new on the namespace or not and an error.
func (n *NodePullSecretsController) secret() (*corev1.Secret, bool, error) {
	// XXX add a constant for the secret name.
	// here and on the empty creation.
	sec, err := n.secretsLister.Get("node-pullsecrets", metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return nil, false, err
	}

	if errors.IsNotFound(err) {
		// XXX use a .dockercfg type secret, as we may have
		// only one secret for the node pull secrets anyway.
		// merge if more than one?
		sec = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "node-pullsecrets",
				Namespace: "openshift-image-registry",
			},
		}
	}

	if sec.Data == nil {
		sec.Data = make(map[string][]byte)
	}

	return sec, errors.IsNotFound(err), nil
}

// updateSecret copies the content of the node pull secrets into a kubernetes
// secret. Returns if something was updated on the secret or not.
func (n *NodePullSecretsController) updateSecret(sec *corev1.Secret, cfg *mcfgv1.MachineConfig) bool {
	var original []byte
	var changed bool

	if content, ok := sec.Data["config.json"]; ok {
		copy(original, content)
	}

	for _, file := range cfg.Spec.Config.Storage.Files {
		// XXX add a constant for this path.
		if file.Path != "/var/lib/kubelet/config.json" {
			continue
		}

		contents, err := dataurl.DecodeString(file.Contents.Source)
		if err != nil {
			klog.Errorf("error extracting machine config file data: %v", err)
			continue
		}

		changed = !bytes.Equal(original, contents.Data)
		sec.Data["config.json"] = contents.Data
	}

	return changed
}

// Run starts this controller, returns only when it is time to die.
func (n *NodePullSecretsController) Run(stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer n.queue.ShutDown()

	klog.Infof("Starting NodePullSecretsController")
	if !cache.WaitForCacheSync(stopCh, n.pools.Informer().HasSynced) {
		return
	}

	go wait.Until(n.loop, time.Second, stopCh)

	klog.Infof("Started NodePullSecretsController")
	<-stopCh
	klog.Infof("Shutting down NodePullSecretsController")
}

// OnAdd adds an event to the queue every time a MachineConfigPool is created.
func (n *NodePullSecretsController) OnAdd(interface{}) {
	n.queue.Add("machineconfigpool")
}

// OnDelete adds an event to the queue every time a MachineConfigPool is
// deleted.
func (n *NodePullSecretsController) OnDelete(interface{}) {
	n.queue.Add("machineconfigpool")
}

// OnUpdate adds an event to the queue every time a MachineConfigPool is
// updated.
func (n *NodePullSecretsController) OnUpdate(interface{}, interface{}) {
	n.queue.Add("machineconfigpool")
}
