package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sirupsen/logrus"
	"github.com/tsuru/remesher/pkg/controller"

	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	stopCh := setupSignalHandler()
	var (
		masterURL         string
		kubeconfig        string
		neighborhoodLabel string
		numWorkers        int
		logLevel          string
		port              string
	)

	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&neighborhoodLabel, "neighborhood-label", "", "The label to use when grouping nodes in the mesh.")
	flag.IntVar(&numWorkers, "num-workers", 1, "The number of workers processing the work queue.")
	flag.StringVar(&logLevel, "log-level", "info", "The log level.")
	flag.StringVar(&port, "metrics-port", "8081", "Metrics port to listen on.")
	flag.Parse()

	log := logrus.New()
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("unable to parse log level %q: %v", logLevel, err)
	}
	log.SetLevel(level)

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		log.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		log.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	calicoClient, err := calicoclientv3.NewFromEnv()
	if err != nil {
		log.Fatalf("Error building calico client: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)

	ctl := controller.NewController(
		kubeClient,
		kubeInformerFactory,
		logrus.NewEntry(log),
		neighborhoodLabel,
		calicoClient,
		prometheus.DefaultRegisterer,
	)

	go kubeInformerFactory.Start(stopCh)
	go servePrometheusMetrics(port, stopCh, log)

	if err = ctl.Run(numWorkers, stopCh); err != nil {
		log.Fatalf("Error running controller: %s", err.Error())
	}
}

func setupSignalHandler() (stopCh <-chan struct{}) {
	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		close(stop)
	}()

	return stop
}

func servePrometheusMetrics(port string, stopCh <-chan struct{}, logger *logrus.Logger) {
	var server *http.Server
	go func(server *http.Server) {
		logger.WithField("port", port).Info("Starting prometheus metrics endpoint")
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		server = &http.Server{Addr: fmt.Sprintf(":%v", port), Handler: mux}
		err := server.ListenAndServe()
		if err == http.ErrServerClosed {
			logger.Info("Prometheus metrics endpoint server closed")
			return
		}
		logger.WithError(err).Error("Prometheus metrics endpoint failed, trying to restart it...")
		time.Sleep(1 * time.Second)
	}(server)

	<-stopCh
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	err := server.Shutdown(ctx)
	cancel()
	if err != nil {
		logger.WithError(err).Errorf("Prometheus metrics endpoint shutdown failed")
	}
}
