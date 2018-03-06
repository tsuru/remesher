package main

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/client-go/tools/leaderelection"

	"context"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tsuru/remesher/pkg/controller"
	"github.com/tsuru/remesher/pkg/k8s"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var Version = "0.0.1"

var (
	cfgFile string
)

func init() {
	cobra.OnInitialize(initConfig, initLogging)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "configfile", "", "Config file (default is $HOME/.remesher.yaml)")
	rootCmd.PersistentFlags().String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	rootCmd.PersistentFlags().String("master-url", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	rootCmd.PersistentFlags().String("neighborhood-label", "", "The label to use when grouping nodes in the mesh.")
	rootCmd.PersistentFlags().Int("num-workers", 1, "The number of workers processing the work queue.")
	rootCmd.PersistentFlags().String("log-level", "info", "The log level.")
	rootCmd.PersistentFlags().String("metrics-port", "8081", "Metrics port to listen on.")
	rootCmd.PersistentFlags().String("namespace", "kube-system", "Namespace used to create resources.")
	rootCmd.PersistentFlags().Duration("leader-elect.lease-duration", time.Second*30,
		"The duration that non-leader candidates will wait after observing a leadership "+
			"renewal until attempting to acquire leadership of a led but unrenewed leader "+
			"slot. This is effectively the maximum duration that a leader can be stopped "+
			"before it is replaced by another candidate.")
	rootCmd.PersistentFlags().Duration("leader-elect.renew-deadline", time.Second*10,
		"The interval between attempts by the acting master to renew a leadership slot "+
			"before it stops leading. This must be less than or equal to the lease duration. ")
	rootCmd.PersistentFlags().Duration("leader-elect.retry-period", time.Second*15,
		"The duration the clients should wait between attempting acquisition and renewal of a leadership.")
}

func initConfig() {
	logrus.SetLevel(logrus.DebugLevel)

	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	}

	viper.SetConfigName(".remesher")
	viper.AddConfigPath("$HOME")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		logrus.Fatalf("unable to bind flags to viper: %v\n", err)
	}

	if err := viper.ReadInConfig(); err == nil {
		logrus.Info("Using config file:", viper.ConfigFileUsed())
	}
}

func initLogging() {
	if viper.GetBool("debug") {
		logrus.SetLevel(logrus.DebugLevel)
		return
	}
	level := logrus.InfoLevel
	configLevel := viper.GetString("log-level")
	if configLevel != "" {
		var err error
		level, err = logrus.ParseLevel(configLevel)
		if err != nil {
			logrus.Fatalf("invalid loglevel: %v", err)
		}
	}
	logrus.SetLevel(level)
}

var rootCmd = &cobra.Command{
	Version: Version,
	Use:     "remesher",
	Run: func(cmd *cobra.Command, args []string) {
		stopCh := setupSignalHandler()
		log := logrus.New()

		kubeClient, err := k8s.NewClientset(viper.GetString("master-url"), viper.GetString("kubeconfig"))
		if err != nil {
			log.Fatalf("Error creating kubernetes client: %v", err)
		}
		calicoClient, err := calicoclientv3.NewFromEnv()
		if err != nil {
			log.Fatalf("Error creating calico client: %v", err)
		}

		go servePrometheusMetrics(viper.GetString("metrics-port"), stopCh, log)
		controller.Start(controller.Config{
			KubeClient:        kubeClient,
			CalicoClient:      calicoClient,
			Logger:            logrus.NewEntry(log),
			Namespace:         viper.GetString("namespace"),
			NeighborhoodLabel: viper.GetString("neighborhood-label"),
			NumWorkers:        viper.GetInt("num-workers"),
			MetricsRegisterer: prometheus.DefaultRegisterer,
			LeaderElectionConfig: leaderelection.LeaderElectionConfig{
				RenewDeadline: viper.GetDuration("leader-elect.renew-deadline"),
				RetryPeriod:   viper.GetDuration("leader-elect.retry-period"),
				LeaseDuration: viper.GetDuration("leader-elect.lease-duration"),
			},
		}, stopCh)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
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
