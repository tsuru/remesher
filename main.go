package main

import (
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"

	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	stopCh := setupSignalHandler()
	var (
		masterURL      string
		kubeconfig     string
		neighborsLabel string
	)

	flag.StringVar(&kubeconfig, "kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&masterURL, "master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	flag.StringVar(&neighborsLabel, "neighbors-label", "", "The label to use when grouping nodes in the mesh.")
	flag.Parse()

	entry := logrus.NewEntry(logrus.New())

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		entry.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		entry.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	calicoClient, err := calicoclientv3.NewFromEnv()
	if err != nil {
		entry.Fatalf("Error building calico client: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*30)

	controller := NewController(kubeClient, kubeInformerFactory, entry, neighborsLabel, calicoClient)

	go kubeInformerFactory.Start(stopCh)

	if err = controller.Run(2, stopCh); err != nil {
		entry.Fatalf("Error running controller: %s", err.Error())
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
