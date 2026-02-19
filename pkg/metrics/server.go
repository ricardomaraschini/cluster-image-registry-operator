package metrics

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
	"k8s.io/klog/v2"
)

// Server represents a metrics server that exposes Prometheus metrics over
// HTTPS with configurable TLS settings.
type Server struct {
	tlsCRT     string
	tlsKey     string
	httpServer *http.Server
}

// NewServer creates a new metrics server with the specified TLS certificates
// and serving configuration. Returns an error if the TLS version or cipher
// suites are invalid.
func NewServer(crt, key string, servinfo configv1.HTTPServingInfo) (*Server, error) {
	minTLSVersion, err := crypto.TLSVersion(servinfo.MinTLSVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to parse min tls version: %w", err)
	}

	var suites []uint16
	for _, suite := range servinfo.CipherSuites {
		tmp, err := crypto.CipherSuite(suite)
		if err != nil {
			return nil, fmt.Errorf("failed to parse suite: %w", err)
		}
		suites = append(suites, tmp)
	}

	handler := promhttp.HandlerFor(
		registry, promhttp.HandlerOpts{
			ErrorHandling: promhttp.HTTPErrorOnError,
		},
	)

	router := http.NewServeMux()
	router.Handle("/metrics", handler)

	return &Server{
		tlsCRT: crt,
		tlsKey: key,
		httpServer: &http.Server{
			Addr:    servinfo.BindAddress,
			Handler: router,
			TLSConfig: &tls.Config{
				MinVersion:   minTLSVersion,
				CipherSuites: suites,
			},
			TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}, // disable HTTP/2
		},
	}, nil
}

// Run starts the metrics server in a background goroutine. The server
// listens on the configured bind address and serves Prometheus metrics
// at the /metrics endpoint over HTTPS.
func (s *Server) Run() {
	go func() {
		if err := s.httpServer.ListenAndServeTLS(s.tlsCRT, s.tlsKey); err != nil {
			if err != http.ErrServerClosed {
				klog.Errorf("error starting metrics server: %v", err)
			}
		}
	}()
}

// Stop immediately shuts down the metrics server. It is safe to call Stop on a
// server that has not been started. Returns an error if the server fails to
// close.
func (s *Server) Stop() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
}

// StorageReconfigured keeps track of the number of times the operator got its
// underlying storage reconfigured.
func StorageReconfigured() {
	storageReconfigured.Inc()
}

// ImagePrunerInstallStatus reports the installation state of automatic image pruner CronJob to Prometheus
func ImagePrunerInstallStatus(installed bool, enabled bool) {
	if !installed {
		imagePrunerInstallStatus.Set(0)
		return
	}
	if !enabled {
		imagePrunerInstallStatus.Set(1)
		return
	}
	imagePrunerInstallStatus.Set(2)
}

// ReportOpenShiftImageStreamTags reports the amount of seen ImageStream tags existing in openshift
// namespaces. Receives the total of 'imported' and 'pushed' image streams tags.
func ReportOpenShiftImageStreamTags(imported float64, pushed float64) {
	imageStreamTags.WithLabelValues("imported", "openshift").Set(imported)
	imageStreamTags.WithLabelValues("pushed", "openshift").Set(pushed)
}

// ReportOtherImageStreamTags reports the amount of seen ImageStream tags existing outside the
// openshift namespaces. Receives the total of 'imported' and 'pushed' image streams tags.
func ReportOtherImageStreamTags(imported float64, pushed float64) {
	imageStreamTags.WithLabelValues("imported", "other").Set(imported)
	imageStreamTags.WithLabelValues("pushed", "other").Set(pushed)
}

// ReportStorageType sets the storage in use.
func ReportStorageType(stype string) {
	storageType.WithLabelValues(stype).Set(1)
}

// AzureKeyCacheHit registers a hit on Azure key cache.
func AzureKeyCacheHit() {
	azurePrimaryKeyCache.With(map[string]string{"result": "hit"}).Inc()
}

// AzureKeyCacheMiss registers a miss on Azure key cache.
func AzureKeyCacheMiss() {
	azurePrimaryKeyCache.With(map[string]string{"result": "miss"}).Inc()
}
