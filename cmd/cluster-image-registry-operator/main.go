package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1alpha1 "github.com/openshift/api/operator/v1alpha1"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	kubeyaml "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/metrics"
	"github.com/openshift/cluster-image-registry-operator/pkg/operator"
	"github.com/openshift/cluster-image-registry-operator/pkg/signals"
	"github.com/openshift/cluster-image-registry-operator/pkg/version"
)

var (
	controllerConfig string
	kubeconfig       string
	filesToWatch     []string
)

func printVersion() {
	klog.Infof("Cluster Image Registry Operator Version: %s", version.Version)
	klog.Infof("Go Version: %s", runtime.Version())
	klog.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
}

// readAndParseControllerConfig reads the controller configuration file and
// parses it into a GenericOperatorConfig. XXX If the provided path is empty
// then it returns a default GenericOperatorConfig, this is needed to make
// the introduction of the config file requirement possible.
func readAndParseControllerConfig(path string) (*operatorv1alpha1.GenericOperatorConfig, error) {
	if path == "" {
		return &operatorv1alpha1.GenericOperatorConfig{
			ServingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress: "0.0.0.0:60000",
				},
			},
		}, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	config := &operatorv1alpha1.GenericOperatorConfig{}
	if err := kubeyaml.Unmarshal(content, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config content: %w", err)
	}

	return config, nil
}

func main() {
	klogFlags := flag.NewFlagSet("klog", flag.ExitOnError)
	klog.InitFlags(klogFlags)
	if logstderr := klogFlags.Lookup("logtostderr"); logstderr != nil {
		_ = logstderr.Value.Set("true")
	}

	watchedFileChanged := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	stopCh := signals.SetupSignalHandler()
	go func() {
		defer cancel()
		select {
		case <-stopCh:
			klog.Infof("Received SIGTERM or SIGINT signal, shutting down the operator.")
		case <-watchedFileChanged:
			klog.Infof("Watched file changed, shutting down the operator.")
		}
	}()

	cmd := &cobra.Command{
		Use:   "cluster-image-registry-operator",
		Short: "OpenShift cluster image registry operator",
		Run: func(cmd *cobra.Command, args []string) {
			ctrl := controllercmd.NewController(
				"image-registry-operator",
				func(ctx context.Context, cctx *controllercmd.ControllerContext) error {
					printVersion()
					config, err := readAndParseControllerConfig(controllerConfig)
					if err != nil {
						return fmt.Errorf("failed to read config: %w", err)
					}

					klog.Infof("Watching files %v...", filesToWatch)

					metricsServer, err := metrics.NewServer(
						"/etc/secrets/tls.crt",
						"/etc/secrets/tls.key",
						config.ServingInfo,
					)
					if err != nil {
						return fmt.Errorf("failed to create metrics server: %w", err)
					}
					metricsServer.Run()

					return operator.RunOperator(ctx, cctx.KubeConfig)
				},
				clock.RealClock{},
			).WithKubeConfigFile(
				kubeconfig, nil,
			).WithLeaderElection(
				configv1.LeaderElection{},
				defaults.ImageRegistryOperatorNamespace,
				"openshift-master-controllers",
			).WithRestartOnChange(
				watchedFileChanged, nil, filesToWatch...,
			)

			if err := ctrl.Run(ctx, nil); err != nil {
				log.Fatal(err)
			}
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster")
	cmd.Flags().StringArrayVar(&filesToWatch, "files", []string{}, "List of files to watch")
	cmd.Flags().StringVar(&controllerConfig, "config", "", "Path to the controller config file")

	if err := cmd.Execute(); err != nil {
		klog.Errorf("%v", err)
		os.Exit(1)
	}
}
