package controller

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/hashicorp/go-multierror"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/projectcalico/libcalico-go/lib/client"
	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	calicoerrors "github.com/projectcalico/libcalico-go/lib/errors"
	"github.com/projectcalico/libcalico-go/lib/options"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const (
	controllerAgentName   = "remesher-controller"
	maxRetries            = 5
	masterLabel           = "node-role.kubernetes.io/master"
	globalLabel           = "remesher.tsuru.io/global"
	asNumber              = client.GlobalDefaultASNumber
	remesherManagedLabel  = "remesher.tsuru.io/managed"
	remesherPeerNodeLabel = "remesher.tsuru.io/peer-node"
	calicoTimeout         = time.Second * 5

	// BGPPeersSyncFailed is the reason used on events to denote a failed sync
	BGPPeersSyncFailed = "BGPPeersSyncFailed"

	// BGPPeersSyncSuccess is the reason used on events to denote a successful sync
	BGPPeersSyncSuccess = "BGPPeersSyncSuccess"
)

var (
	kubeNameRegex = regexp.MustCompile(`(?i)[^a-z0-9.-]`)
)

type controller struct {
	kubeclientset kubernetes.Interface

	nodesInformer coreinformers.NodeInformer
	nodesSynced   cache.InformerSynced

	workqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder

	logger *logrus.Entry

	neighborhoodLabel string

	//TODO: extract this to another pkg and require a minimal interface here
	calicoClient calicoclientv3.Interface

	metricsRegisterer prometheus.Registerer
	errorsCounter     prometheus.Counter
	workqueueDuration prometheus.Histogram
	workqueueLen      prometheus.GaugeFunc
}

// Config is a container for the Controller configuration options
type Config struct {
	CalicoClient         calicoclientv3.Interface
	KubeClient           kubernetes.Interface
	Logger               *logrus.Entry
	Namespace            string
	NumWorkers           int
	NeighborhoodLabel    string
	MetricsRegisterer    prometheus.Registerer
	LeaderElectionConfig leaderelection.LeaderElectionConfig
}

// Start starts the Remesher controller that watches for node events and reconcile the BGPPeers in Calico
// The controller uses leader election to make sure a single instance is handling events
func Start(c Config, stopCh <-chan struct{}) error {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(c.Logger.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: c.KubeClient.CoreV1().Events(c.Namespace)})
	recorder := eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName})

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(c.KubeClient, time.Second*30)
	run := func(stopCh <-chan struct{}) {
		nodeInformer := kubeInformerFactory.Core().V1().Nodes()
		ctl := newController(
			c.KubeClient,
			nodeInformer,
			recorder,
			c.Logger,
			c.NeighborhoodLabel,
			c.CalicoClient,
			c.MetricsRegisterer,
		)
		// this needs to be done after a call to nodeInformer.Informer()
		go kubeInformerFactory.Start(stopCh)
		if err := ctl.Run(c.NumWorkers, stopCh); err != nil {
			c.Logger.WithError(err).Warn("failure running controller")
		}
	}

	id, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("failed to get hostname: %v", err)
	}
	c.LeaderElectionConfig.Lock = &resourcelock.EndpointsLock{
		EndpointsMeta: metav1.ObjectMeta{
			Namespace: c.Namespace,
			Name:      controllerAgentName,
		},
		Client: c.KubeClient.CoreV1(),
		LockConfig: resourcelock.ResourceLockConfig{
			Identity:      id + "-remesher",
			EventRecorder: recorder,
		},
	}
	c.LeaderElectionConfig.Callbacks = leaderelection.LeaderCallbacks{
		OnStartedLeading: run,
		OnStoppedLeading: func() {
			c.Logger.Info("leaderelection lost")
		},
	}
	leaderelection.RunOrDie(c.LeaderElectionConfig)
	return nil
}

// NewController returns a new controller
func newController(kubeclientset kubernetes.Interface,
	nodeInformer coreinformers.NodeInformer,
	recorder record.EventRecorder,
	logger *logrus.Entry,
	label string,
	calicoClient calicoclientv3.Interface,
	metricsRegistry prometheus.Registerer) *controller {

	controller := &controller{
		kubeclientset:     kubeclientset,
		nodesInformer:     nodeInformer,
		nodesSynced:       nodeInformer.Informer().HasSynced,
		workqueue:         workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder:          recorder,
		logger:            logger,
		neighborhoodLabel: label,
		calicoClient:      calicoClient,
	}

	logger.Info("Setting up event handlers")
	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(key)
				controller.logger.WithField("op", "add").Infof("added %s to workqueue", key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(key)
				controller.logger.WithField("op", "delete").Infof("added %s to workqueue", key)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				controller.workqueue.Add(key)
				controller.logger.WithField("op", "update").Infof("added %s to workqueue", key)
			}
		},
	})

	logger.Info("Registering Prometheus metrics")
	controller.metricsRegisterer = metricsRegistry
	controller.workqueueDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "remesher_controller_process_duration",
			Help: "The duration of the item processing in seconds.",
		},
	)
	controller.errorsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "remesher_controller_errors_total",
			Help: "The total number of errors processing items.",
		},
	)
	controller.workqueueLen = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "remesher_controller_workqueue_current_items",
		Help: "The current count of items on the workqueue.",
	}, func() float64 {
		return float64(controller.workqueue.Len())
	})
	controller.metricsRegisterer.MustRegister(controller.workqueueDuration, controller.errorsCounter, controller.workqueueLen)

	return controller
}

// Run runs the controller which starts workers to process the work queue
func (c *controller) Run(numWorkers int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()
	defer c.shutdown()

	c.logger.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.nodesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	c.logger.Info("Starting workers")
	for i := 0; i < numWorkers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	c.logger.Info("Started workers")
	<-stopCh
	c.logger.Info("Shutting down workers")

	return nil
}

func (c *controller) shutdown() {
	c.logger.Info("Shutting down prometheus collectors")
	c.metricsRegisterer.Unregister(c.workqueueLen)
	c.metricsRegisterer.Unregister(c.workqueueDuration)
	c.metricsRegisterer.Unregister(c.errorsCounter)
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *controller) runWorker() {
	for c.processNextItem() {
	}
}

// processNextWorkItem deals with one key off the queue.  It returns false
// when it's time to quit.
func (c *controller) processNextItem() bool {
	key, quit := c.workqueue.Get()
	if quit {
		return false
	}

	defer c.workqueue.Done(key)

	now := time.Now()
	err := c.processItem(key.(string))
	c.workqueueDuration.Observe(time.Since(now).Seconds())

	if err == nil {
		c.workqueue.Forget(key)
	} else if c.workqueue.NumRequeues(key) < maxRetries {
		c.errorsCounter.Inc()
		c.logger.Errorf("Error processing %s (will retry): %v", key, err)
		c.workqueue.AddRateLimited(key)
	} else {
		c.errorsCounter.Inc()
		c.logger.Errorf("Error processing %s (giving up): %v", key, err)
		c.workqueue.Forget(key)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *controller) processItem(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}
	logger := c.logger.WithField("node", name)

	node, err := c.nodesInformer.Lister().Get(name)
	if err != nil {
		if kubeerrors.IsNotFound(err) {
			_, errRem := c.removeNode(name, logger)
			return errRem
		}
		return err
	}

	changed, err := c.addNode(node, logger)
	if err != nil {
		c.recorder.Eventf(node, corev1.EventTypeWarning, BGPPeersSyncFailed, "Error: %v", err)
		return err
	}
	if changed > 0 {
		c.recorder.Event(node, corev1.EventTypeNormal, BGPPeersSyncSuccess, "BGPPeers synced successfully")
	}
	return nil
}

func (c *controller) addNode(node *corev1.Node, logger *logrus.Entry) (int, error) {
	logger.Info("handling add operation")

	neightbors, err := c.getBGPNeighbors(node)
	if err != nil {
		return 0, fmt.Errorf("failed to get node neighbors: %v", err)
	}
	expectedPeers := buildMesh(node, neightbors)

	currPeers, err := c.getCurrentBGPPeers(node.Name, true)
	if err != nil {
		return 0, fmt.Errorf("failed to get current peers: %v", err)
	}
	return c.reconcile(currPeers, expectedPeers, logger)
}

func (c *controller) reconcile(current, desired []calicoapiv3.BGPPeer, logger *logrus.Entry) (int, error) {
	logger.Debugf("current peers: %+#v - expected peers: %+#v", current, desired)
	toAdd, toRemove := diff(current, desired)
	logger.Debugf("toAdd: %+#v - toRemove: %+#v", toAdd, toRemove)

	var errors *multierror.Error
	var added int
	for _, p := range toAdd {
		ctx, cancel := context.WithTimeout(context.Background(), calicoTimeout)
		_, err := c.calicoClient.BGPPeers().Create(ctx, &p, options.SetOptions{})
		cancel()
		if err != nil {
			if _, ok := err.(calicoerrors.ErrorResourceAlreadyExists); ok {
				logger.Infof("ignoring error creating bgpPeer %v: %v", p.Name, err)
				continue
			}
			errors = multierror.Append(errors, err)
			continue
		}
		added++
	}
	removed, err := c.removePeers(toRemove, logger)
	errors = multierror.Append(errors, err)
	return removed + added, errors.ErrorOrNil()
}

func (c *controller) removeNode(name string, logger *logrus.Entry) (int, error) {
	logger.Info("handling remove operation")
	currPeers, err := c.getCurrentBGPPeers(name, false)
	if err != nil {
		return 0, err
	}
	return c.removePeers(currPeers, logger)
}

func (c *controller) removePeers(peers []calicoapiv3.BGPPeer, logger *logrus.Entry) (int, error) {
	var errors *multierror.Error
	var removed int
	for _, p := range peers {
		if _, ok := p.Labels[remesherManagedLabel]; !ok {
			logger.Infof("skipping peer %v: unmanaged due to missing label %q", p.Name, remesherManagedLabel)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), calicoTimeout)
		_, err := c.calicoClient.BGPPeers().Delete(ctx, p.Name, options.DeleteOptions{})
		cancel()
		if err != nil {
			if _, ok := err.(calicoerrors.ErrorResourceDoesNotExist); ok {
				logger.Infof("ignoring error deleting bgpPeer %v: %v", p.Name, err)
				continue
			}
			errors = multierror.Append(errors, err)
			continue
		}
		removed++
	}
	return removed, errors.ErrorOrNil()
}

// getCurrentBGPPeers returns all BGPPeers that directly refer the node in calico (both ways)
func (c *controller) getCurrentBGPPeers(nodeName string, includeAllGlobals bool) ([]calicoapiv3.BGPPeer, error) {
	// TODO: we should have a way to cache bgppeers to reduce the number of api calls
	// perhaps using the Kubernetes API directly thru listers with caching instead of
	// using the calico client (which means we would only support kubernetes backend)
	ctx, cancel := context.WithTimeout(context.Background(), calicoTimeout)
	defer cancel()
	list, err := c.calicoClient.BGPPeers().List(ctx, options.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list bgp peers for node %q: %v", nodeName, err)
	}
	var peers []calicoapiv3.BGPPeer
	for _, p := range list.Items {
		if (includeAllGlobals && p.Spec.Node == "") || p.Annotations[remesherPeerNodeLabel] == nodeName {
			peers = append(peers, p)
		}
		if p.Spec.Node == nodeName {
			peers = append(peers, p)
		}
	}
	return peers, nil
}

func (c *controller) getBGPNeighbors(node *corev1.Node) ([]*corev1.Node, error) {
	if isGlobal(node) {
		return c.nodesInformer.Lister().List(labels.Everything())
	}
	v, ok := node.Labels[c.neighborhoodLabel]
	if !ok {
		c.logger.WithField("node", node.Name).Infof("missing neighborsLabel %q", c.neighborhoodLabel)
		// TODO: if the label is missing, consider returning all nodes (except for the masters/global peers)
		return nil, nil
	}
	queries := map[string]string{
		c.neighborhoodLabel: v,
		masterLabel:         "true",
		globalLabel:         "true",
	}
	var nodes []*corev1.Node
	for k, v := range queries {
		n, err := c.nodesInformer.Lister().List(labels.SelectorFromSet(map[string]string{k: v}))
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n...)
	}
	return nodes, nil
}
