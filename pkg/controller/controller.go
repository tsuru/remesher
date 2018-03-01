package controller

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/labels"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/projectcalico/libcalico-go/lib/client"
	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	calicoerrors "github.com/projectcalico/libcalico-go/lib/errors"
	"github.com/projectcalico/libcalico-go/lib/options"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	controllerAgentName   = "remesher-controller"
	maxRetries            = 5
	masterLabel           = "node-role.kubernetes.io/master"
	globalLabel           = "remesher.tsuru.io/global"
	asNumber              = client.GlobalDefaultASNumber
	remesherManagedLabel  = "remesher.tsuru.io/managed"
	remesherPeerNodeLabel = "remesher.tsuru.io/peer-node"
	calicoTimeout         = time.Second * 5
)

var kubeNameRegex = regexp.MustCompile(`(?i)[^a-z0-9.-]`)

// Controller is a controller that watches for node changes and updates BGPPeers resources
type Controller struct {
	kubeclientset kubernetes.Interface

	nodesInformer coreinformers.NodeInformer
	nodesSynced   cache.InformerSynced

	workqueue workqueue.RateLimitingInterface

	recorder record.EventRecorder

	logger *logrus.Entry

	neighborsLabel string

	//TODO: extract this to another pkg and require a minimal interface here
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
	logger := c.logger.WithField("node", node.Name)
	logger.Info("handling add operation")
	currPeers, err := c.getCurrentBGPPeers(node.Name)
	if err != nil {
		return fmt.Errorf("failed to get current peers: %v", err)
	}
	var expectedPeers []calicoapiv3.BGPPeer
	if isGlobal(node) {
		logger.Infof("should be added as global peer")
		expectedPeers = append(expectedPeers, buildPeer(nil, node))
	} else {
		neightbors, err := c.getBGPNeighbors(node)
		if err != nil {
			return fmt.Errorf("failed to get node neighbors: %v", err)
		}
		expectedPeers = c.buildMesh(node, neightbors)
	}
	logger.Infof("current peers: %+#v - expected peers: %+#v", currPeers, expectedPeers)
	toAdd, toRemove := deltaPeers(currPeers, expectedPeers)
	logger.Infof("toAdd: %+#v - toRemove: %+#v", toAdd, toRemove)

	for _, p := range toAdd {
		ctx, cancel := context.WithTimeout(context.Background(), calicoTimeout)
		_, err := c.calicoClient.BGPPeers().Create(ctx, &p, options.SetOptions{})
		cancel()
		if err != nil {
			if _, ok := err.(calicoerrors.ErrorResourceAlreadyExists); ok {
				logger.Infof("ignoring error creating bgpPeer %v: %v", p.Name, err)
				continue
			}
			// TODO: add to multierror and return the multierror
			logger.Errorf("failed to create bgpPeer %v: %v", p, err)
		}
	}

	return c.removePeers(toRemove, logger)
}

func (c *Controller) removeNode(name string) error {
	logger := c.logger.WithField("node", name)
	logger.Info("handling remove operation")
	currPeers, err := c.getCurrentBGPPeers(name)
	if err != nil {
		return err
	}
	return c.removePeers(currPeers, logger)
}

func (c *Controller) removePeers(peers []calicoapiv3.BGPPeer, logger *logrus.Entry) error {
	for _, p := range peers {
		if _, ok := p.Annotations[remesherManagedLabel]; !ok {
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
			// TODO: add to multierror and return the multierror
			logger.Errorf("failed to remove bgpPeer %v: %v", p.Name, err)
		}
	}
	return nil
}

func (c *Controller) buildMesh(node *corev1.Node, toNodes []*corev1.Node) []calicoapiv3.BGPPeer {
	var peers []calicoapiv3.BGPPeer
	for _, n := range toNodes {
		if node.Name == n.Name {
			continue
		}
		peers = append(peers, buildPeer(node, n), buildPeer(n, node))
	}
	return peers
}

func deltaPeers(current, desired []calicoapiv3.BGPPeer) (toAdd []calicoapiv3.BGPPeer, toRemove []calicoapiv3.BGPPeer) {
	currMap := make(map[calicoapiv3.BGPPeerSpec]calicoapiv3.BGPPeer)
	for _, c := range current {
		currMap[c.Spec] = c
	}
	for _, d := range desired {
		if _, ok := currMap[d.Spec]; ok {
			delete(currMap, d.Spec)
			continue
		}
		toAdd = append(toAdd, d)
	}
	for _, c := range currMap {
		toRemove = append(toRemove, c)
	}
	return toAdd, toRemove
}

// buildPeer builds a BGPPeer using from as Node and to IP as PeerIP
// creates a global bgpPEer if from is not set
func buildPeer(from, to *corev1.Node) calicoapiv3.BGPPeer {
	//TODO: consider nodes with multiple addresses, maybe thru an annotation on the node?
	var name, node string
	if from != nil {
		name = strings.ToLower(kubeNameRegex.ReplaceAllString(from.Name+"-"+to.Name, "-"))
		node = from.Name
	} else {
		name = strings.ToLower(kubeNameRegex.ReplaceAllString("global-"+to.Name, "-"))
	}
	return calicoapiv3.BGPPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "remesher-" + name,
			Annotations: map[string]string{
				remesherManagedLabel:  "true",
				remesherPeerNodeLabel: to.Name,
			},
			Labels: map[string]string{
				remesherManagedLabel:  "true",
				remesherPeerNodeLabel: to.Name,
			},
		},
		Spec: calicoapiv3.BGPPeerSpec{
			Node:     node,
			PeerIP:   to.Status.Addresses[0].Address,
			ASNumber: asNumber,
		},
	}
}

func isGlobal(node *corev1.Node) bool {
	_, master := node.Labels[masterLabel]
	_, global := node.Labels[globalLabel]
	return master || global
}

// getCurrentBGPPeers returns all BGPPeers that refer to the node in calico (both ways)
func (c *Controller) getCurrentBGPPeers(nodeName string) ([]calicoapiv3.BGPPeer, error) {
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
		if p.Spec.Node == nodeName || p.Annotations[remesherPeerNodeLabel] == nodeName {
			peers = append(peers, p)
			continue
		}
	}
	return peers, nil
}

func (c *Controller) getBGPNeighbors(node *corev1.Node) ([]*corev1.Node, error) {
	v, ok := node.Labels[c.neighborsLabel]
	if !ok {
		c.logger.WithField("node", node.Name).Infof("missing neighborsLabel %q", c.neighborsLabel)
		// TODO: if the label is missing, consider returning all nodes (except for the masters/global peers)
		return nil, nil
	}
	return c.nodesInformer.Lister().List(labels.SelectorFromSet(map[string]string{c.neighborsLabel: v}))
}
