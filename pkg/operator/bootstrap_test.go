package operator

// XXX We are not bootstrapping any config on our main operator, we need to migrate this
// test to use StorageOperator instead (storageoperator.go)
// func TestBootstrapAWS(t *testing.T) {
//	ctx, cancel := context.WithCancel(context.Background())
//	defer cancel()
//
//	configObjects := []runtime.Object{
//		&configv1.Infrastructure{
//			ObjectMeta: metav1.ObjectMeta{
//				Name: "cluster",
//			},
//			Status: configv1.InfrastructureStatus{
//				PlatformStatus: &configv1.PlatformStatus{
//					Type: configv1.AWSPlatformType,
//				},
//			},
//		},
//	}
//
//	configClient := configfakeclient.NewSimpleClientset(configObjects...)
//	configInformerFactory := configinformers.NewSharedInformerFactory(configClient, 0)
//
//	imageregistryClient := imageregistryfakeclient.NewSimpleClientset()
//	imageregistryInformerFactory := imageregistryinformers.NewSharedInformerFactory(imageregistryClient, 0)
//
//	c := &Controller{
//		listers: &client.Listers{
//			Infrastructures: configInformerFactory.Config().V1().Infrastructures().Lister(),
//			RegistryConfigs: imageregistryInformerFactory.Imageregistry().V1().Configs().Lister(),
//		},
//		clients: &client.Clients{
//			RegOp: imageregistryClient,
//		},
//	}
//
//	configInformerFactory.Start(ctx.Done())
//	imageregistryInformerFactory.Start(ctx.Done())
//	configInformerFactory.WaitForCacheSync(ctx.Done())
//	imageregistryInformerFactory.WaitForCacheSync(ctx.Done())
//
//	if err := c.Bootstrap(); err != nil {
//		t.Fatalf("bootstrap failed: %v", err)
//	}
//
//	config, err := imageregistryClient.ImageregistryV1().Configs().Get(ctx, "cluster", metav1.GetOptions{})
//	if err != nil {
//		t.Fatal(err)
//	}
//
//	if config.Spec.HTTPSecret == "" {
//		t.Errorf("got empty spec.httpSecrets, want random string")
//	}
//	config.Spec.HTTPSecret = ""
//
//	expected := imageregistryv1.ImageRegistrySpec{
//		ManagementState: "Managed",
//		Storage: imageregistryv1.ImageRegistryConfigStorage{
//			S3: &imageregistryv1.ImageRegistryConfigStorageS3{},
//		},
//		Replicas:        2,
//		LogLevel:        2,
//		RolloutStrategy: "RollingUpdate",
//	}
//	if !reflect.DeepEqual(config.Spec, expected) {
//		t.Errorf("unexpected config: %s", cmp.Diff(expected, config.Spec))
//	}
// }
