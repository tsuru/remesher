package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/projectcalico/libcalico-go/lib/client"
	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	"github.com/projectcalico/libcalico-go/lib/options"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const (
	controllerAgentName = "remesher-controller"
	maxRetries          = 5
	masterLabel         = "node-role.kubernetes.io/master"
	asNumber            = client.GlobalDefaultASNumber
)

// Controller is a controller that watches for node changes and updates BGPPeers resources
type Controller struct {
	kubeclientset kubernetes.Interface

	nodesInformer coreinformers.NodeInformer
	nodesSynced   cache.InformerSynced

	workqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder

	logger *logrus.Entry

	neighborsLabel string

	calicoClient calicoclientv3.Interface
}

// NewController returns a new controller
func NewController(kubeclientset kubernetes.Interface,
	kubeInformerFactory kubeinformers.SharedInformerFactory,
	logger *logrus.Entry,
	label string,
	calicoClient calicoclientv3.Interface) *Controller {

	nodeInformer := kubeInformerFactory.Core().V1().Nodes()

	logrus.Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logger.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")})

	controller := &Controller{
		kubeclientset:  kubeclientset,
		nodesInformer:  nodeInformer,
		nodesSynced:    nodeInformer.Informer().HasSynced,
		workqueue:      workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		recorder:       eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: controllerAgentName}),
		logger:         logger,
		neighborsLabel: label,
		calicoClient:   calicoClient,
	}

	logrus.Info("Setting up event handlers")

	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(key)
			}
		},
		DeleteFunc: func(obj interface{}) {
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			if err == nil {
				controller.workqueue.Add(key)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(new)
			if err == nil {
				controller.workqueue.Add(key)
			}
		},
	})

	return controller
}

// Run runs the controller which starts workers to process the work queue
func (c *Controller) Run(numWorkers int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()
	// Wait for the caches to be synced before starting workers
	c.logger.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.nodesSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	c.logger.Info("Starting workers")
	// Launch two workers to process Foo resources
	for i := 0; i < numWorkers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	c.logger.Info("Started workers")
	<-stopCh
	c.logger.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

// processNextWorkItem deals with one key off the queue.  It returns false
// when it's time to quit.
func (c *Controller) processNextItem() bool {
	// pull the next work item from queue.  It should be a key we use to lookup
	// something in a cache
	key, quit := c.workqueue.Get()
	if quit {
		return false
	}

	defer c.workqueue.Done(key)

	err := c.processItem(key.(string))

	if err == nil {
		c.workqueue.Forget(key)
	} else if c.workqueue.NumRequeues(key) < maxRetries {
		c.logger.Errorf("Error processing %s (will retry): %v", key, err)
		c.workqueue.AddRateLimited(key)
	} else {
		c.logger.Errorf("Error processing %s (giving up): %v", key, err)
		c.workqueue.Forget(key)
		utilruntime.HandleError(err)
	}

	return true
}

func (c *Controller) processItem(key string) error {
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	node, err := c.nodesInformer.Lister().Get(name)
	if err != nil {
		if kubeerrors.IsNotFound(err) {
			return c.removeNode(name)
		}
		return err
	}

	return c.addNode(node)
}

func (c *Controller) addNode(node *corev1.Node) error {
	c.logger.WithField("node", node.Name).Info("handling add operation")
	currPeers, err := c.getCurrentBGPPeers(node)
	if err != nil {
		return fmt.Errorf("failed to get current peers: %v", err)
	}
	// TODO: support an additional custom label for global peers
	if isMaster(node) {
		c.logger.WithField("node", node.Name).Infof("handling as global peer")
		// TODO: add master nodes as global peers and remove all direct peers
	}
	neightbors, err := c.getBGPNeighbors(node)
	if err != nil {
		return fmt.Errorf("failed to get node neighbors: %v", err)
	}
	expectedPeers := c.buildMesh(node, neightbors)
	c.logger.WithField("node", node.Name).Infof("current peers: %v - expected peers: %v", currPeers, expectedPeers)
	return nil
}

func (c *Controller) removeNode(name string) error {
	c.logger.WithField("node", name).Info("handling remove operation")
	return nil
}

func (c *Controller) buildMesh(node *corev1.Node, toNodes []*corev1.Node) []calicoapiv3.BGPPeer {
	var peers []calicoapiv3.BGPPeer
	for _, n := range toNodes {
		peers = append(peers, buildPeer(node, n), buildPeer(n, node))
	}
	return peers
}

func buildPeer(from, to *corev1.Node) calicoapiv3.BGPPeer {
	//TODO: consider nodes with multiple addresses
	return calicoapiv3.BGPPeer{
		Spec: calicoapiv3.BGPPeerSpec{
			Node:     from.Name,
			PeerIP:   to.Status.Addresses[0].Address,
			ASNumber: asNumber,
		},
	}
}

func isMaster(node *corev1.Node) bool {
	_, ok := node.Labels[masterLabel]
	return ok
}

// getCurrentBGPPeers returns all BGPPeers that refer to the node in calico (both ways)
func (c *Controller) getCurrentBGPPeers(node *corev1.Node) ([]calicoapiv3.BGPPeer, error) {
	// TODO: we should have a way to cache bgppeers to reduce the number of api calls
	// perhaps using the Kubernetes API directly thru listers with caching instead of
	// using the calico client (which means we would only support kubernetes backend)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	list, err := c.calicoClient.BGPPeers().List(ctx, options.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list bgp peers for node %q: %v", node.Name, err)
	}
	var peers []calicoapiv3.BGPPeer
	for _, p := range list.Items {
		if p.Spec.Node == node.Name {
			peers = append(peers, p)
			continue
		}
		for _, addr := range node.Status.Addresses {
			if p.Spec.PeerIP == addr.Address {
				peers = append(peers, p)
				break
			}
		}
	}
	return peers, nil
}

func (c *Controller) getBGPNeighbors(node *corev1.Node) ([]*corev1.Node, error) {
	// TODO: if the labels is missing, consider adding every node as neightbor
	v, ok := node.Labels[c.neighborsLabel]
	if !ok {
		return nil, errors.New("node missing neighborsLabel")
	}
	return c.nodesInformer.Lister().List(labels.SelectorFromSet(map[string]string{c.neighborsLabel: v}))
}
