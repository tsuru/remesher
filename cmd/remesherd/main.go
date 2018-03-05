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

	"github.com/tsuru/remesher/pkg/k8s"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	"github.com/sirupsen/logrus"
	"github.com/tsuru/remesher/pkg/controller"

	kubeinformers "k8s.io/client-go/informers"
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
		namespace         string
	)
	//TODO: consider using viper for this
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&neighborhoodLabel, "neighborhood-label", "", "The label to use when grouping nodes in the mesh.")
	flag.IntVar(&numWorkers, "num-workers", 1, "The number of workers processing the work queue.")
	flag.StringVar(&logLevel, "log-level", "info", "The log level.")
	flag.StringVar(&port, "metrics-port", "8081", "Metrics port to listen on.")
	flag.StringVar(&namespace, "namespace", "kube-system", "Namespace used to create resources.")
	flag.Parse()

	log := logrus.New()
	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		log.Fatalf("unable to parse log level %q: %v", logLevel, err)
	}
	log.SetLevel(level)

	kubeClient, err := k8s.NewClientset(masterURL, kubeconfig)
	if err != nil {
		log.Fatalf("Error creating kubernetes client: %v", err)
	}
	calicoClient, err := calicoclientv3.NewFromEnv()
	if err != nil {
		log.Fatalf("Error creating calico client: %v", err)
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)
	go kubeInformerFactory.Start(stopCh)
	go servePrometheusMetrics(port, stopCh, log)
	controller.Start(controller.Config{
		KubeClient:          kubeClient,
		CalicoClient:        calicoClient,
		Logger:              logrus.NewEntry(log),
		Namespace:           namespace,
		KubeInformerFactory: kubeInformerFactory,
		NeighborhoodLabel:   neighborhoodLabel,
		NumWorkers:          numWorkers,
	})
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
