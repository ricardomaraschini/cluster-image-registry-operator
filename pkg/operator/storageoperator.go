package operator

import (
	"context"
	"reflect"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	registryv1 "github.com/openshift/api/imageregistry/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	registryv1client "github.com/openshift/client-go/imageregistry/clientset/versioned/typed/imageregistry/v1"
	registryv1informers "github.com/openshift/client-go/imageregistry/informers/externalversions/imageregistry/v1"
	registryv1listers "github.com/openshift/client-go/imageregistry/listers/imageregistry/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage"
)

// StorageController keeps track of image registry storage configuration, its role is to
// guarantee that what is specified on Spec.Storage reflects on Status.Storage.
type StorageController struct {
	configLister registryv1listers.ConfigLister
	configClient registryv1client.ConfigInterface
	cachesToSync []cache.InformerSynced
	queue        workqueue.RateLimitingInterface
	listers      *client.Listers
	kubeconfig   *rest.Config
}

// NewStorageController returns a new storage controller.
func NewStorageController(
	configInformer registryv1informers.ConfigInformer,
	configClient registryv1client.ConfigInterface,
	listers *client.Listers,
	kubeconfig *rest.Config,
) *StorageController {
	c := &StorageController{
		configLister: configInformer.Lister(),
		configClient: configClient,
		listers:      listers,
		kubeconfig:   kubeconfig,
		queue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"StorageController",
		),
	}

	configInformer.Informer().AddEventHandler(
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				c.queue.Add("instance")
			},
			UpdateFunc: func(old, new interface{}) {
				c.queue.Add("instance")
			},
			DeleteFunc: func(obj interface{}) {
				c.queue.Add("instance")
			},
		},
	)
	c.cachesToSync = []cache.InformerSynced{configInformer.Informer().HasSynced}
	return c
}

func (c *StorageController) runWorker() {
	for c.processNextWorkItem() {
	}
}

func (c *StorageController) processNextWorkItem() bool {
	obj, shutdown := c.queue.Get()
	if shutdown {
		return false
	}
	defer c.queue.Done(obj)

	klog.V(1).Infof("get event from workqueue")
	if err := c.sync(); err != nil {
		c.queue.AddRateLimited(workqueueKey)
		klog.Errorf("unable to sync StorageController: %s, requeuing", err)
	} else {
		c.queue.Forget(obj)
		klog.Infof("event from workqueue successfully processed")
	}
	return true
}

// bootstrap discovers in what platform we are running on and creates an empty
// storage configuration using the appropriate platform. It also makes sure we
// are setting the storage management state to 'Managed'.
func (c *StorageController) bootstrap() error {
	cfg, err := c.configLister.Get("cluster")
	if err != nil {
		return err
	}
	cfg = cfg.DeepCopy()

	// If we have been configured as Removed there is no need for
	// bootstrapping the storage.
	if cfg.Spec.ManagementState == operatorv1.Removed {
		return nil
	}

	// XXX second return here is the number of replicas, this should not
	// be managed here for sure. Still need to see how to tackle this
	// from the main controller (controller.go) point of view.
	// Every driver should implement a method that would return a default
	// configuration, with all default properties set.
	storageConfig, _, err := storage.GetPlatformStorage(c.listers)
	if err != nil {
		return err
	}
	storageConfig.ManagementState = registryv1.StorageManagementStateManaged

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cfg, err := c.configLister.Get("cluster")
		if err != nil {
			return err
		}
		cfg = cfg.DeepCopy()

		cfg.Spec.Storage = storageConfig

		_, err = c.configClient.Update(context.Background(), cfg, metav1.UpdateOptions{})
		return err
	})
}

// remove erases the underlying storage unit (bucket, container, etc) if
// the operator is managing the storage (StorageManagementStateManaged).
// Status.Storage driver configuration is reset.
func (c *StorageController) remove() error {
	cfg, err := c.configLister.Get("cluster")
	if err != nil {
		return err
	}
	cfg = cfg.DeepCopy()

	// If we are not managing the storage bail out.
	if cfg.Status.Storage.ManagementState != registryv1.StorageManagementStateManaged {
		return nil
	}

	currentStorage := cfg.Status.Storage
	drv, err := storage.NewDriver(&currentStorage, c.kubeconfig, c.listers)
	if err != nil {
		if err != storage.ErrStorageNotConfigured {
			return err
		}

		// If there is no driver configuration on Status.Storage
		// we already deleted it.
		return nil
	}

	if _, err := drv.RemoveStorage(cfg); err != nil {
		return err
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if cfg, err = c.configLister.Get("cluster"); err != nil {
			return err
		}
		cfg = cfg.DeepCopy()

		cfg.Status.Storage = registryv1.ImageRegistryConfigStorage{
			ManagementState: cfg.Spec.Storage.ManagementState,
		}

		_, err = c.configClient.UpdateStatus(
			context.Background(), cfg, metav1.UpdateOptions{},
		)
		return err
	})
}

func (c *StorageController) sync() error {
	cfg, err := c.configLister.Get("cluster")
	if err != nil {
		return err
	}
	cfg = cfg.DeepCopy()

	switch cfg.Spec.ManagementState {
	case operatorv1.Unmanaged:
		return nil
	case operatorv1.Removed:
		return c.remove()
	}

	desiredStorage := cfg.Spec.Storage
	currentStorage := cfg.Status.Storage
	emptyStorageConfig := registryv1.ImageRegistryConfigStorage{}

	if desiredStorage == emptyStorageConfig {
		return c.bootstrap()
	}

	// nothing has changed since the last sync cycle.
	if reflect.DeepEqual(desiredStorage, currentStorage) {
		return nil
	}

	drv, err := storage.NewDriver(&desiredStorage, c.kubeconfig, c.listers)
	if err != nil {
		return err
	}

	if err := drv.CreateStorage(cfg); err != nil {
		return err
	}

	// XXX still need to save Config here. Ideally we would only deal
	// with Status, not with Spec anymore.
	// XXX still need to port the storage reconfigure metric into here.
	return nil
}

// Run starts to process events from the queue. Returns when stopCh is closed.
func (c *StorageController) Run(stopCh <-chan struct{}) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()

	klog.Infof("Starting StorageController")
	if !cache.WaitForCacheSync(stopCh, c.cachesToSync...) {
		return
	}

	go wait.Until(c.runWorker, time.Second, stopCh)

	klog.Infof("Started StorageController")
	<-stopCh
	klog.Infof("Shutting down StorageController")
}
