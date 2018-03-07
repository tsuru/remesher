package calico

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	apiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	calicoerrors "github.com/projectcalico/libcalico-go/lib/errors"
	"github.com/projectcalico/libcalico-go/lib/options"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	opCreate = "create"
	opList   = "list"
	opDelete = "delete"
)

// BGPPeerInterface is a minimal interface that describe the basic operations performed by a BGPPeerClient
type BGPPeerInterface interface {
	Create(peer *apiv3.BGPPeer) error
	Delete(name string) error
	List() (*apiv3.BGPPeerList, error)
}

// BGPPeerClient is a BGPPeerInterface implements automatic timeouts and metrics
type bgpPeerClient struct {
	calicoClient calicoclientv3.Interface
	logger       *logrus.Entry

	timeout time.Duration

	latencies *prometheus.HistogramVec
	errors    *prometheus.CounterVec
}

// NewBGPPeerClient constructs BGPPeerInterface instance registering its metrics and timeouts
func NewBGPPeerClient(logger *logrus.Entry, timeout time.Duration, metricsRegisterer prometheus.Registerer) (BGPPeerInterface, error) {
	calicoClient, err := calicoclientv3.NewFromEnv()
	if err != nil {
		return nil, err
	}
	latencies := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "remesher_calico_operations_duration",
		Help: "The duration of calico requests.",
	}, []string{"operation"})
	if err := metricsRegisterer.Register(latencies); err != nil {
		return nil, err
	}
	errors := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "remesher_calico_errors_total",
		Help: "The total number of errors.",
	}, []string{"operation"})
	if err := metricsRegisterer.Register(errors); err != nil {
		return nil, err
	}
	return &bgpPeerClient{
		calicoClient: calicoClient,
		timeout:      timeout,
		latencies:    latencies,
		errors:       errors,
	}, nil
}

func (c *bgpPeerClient) Create(peer *apiv3.BGPPeer) (err error) {
	defer func(begin time.Time, op string) {
		c.latencies.WithLabelValues(op).Observe(time.Since(begin).Seconds())
		if err != nil {
			c.errors.WithLabelValues(op).Inc()
		}
	}(time.Now(), opCreate)
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	_, err = c.calicoClient.BGPPeers().Create(ctx, peer, options.SetOptions{})
	if _, ok := err.(calicoerrors.ErrorResourceAlreadyExists); ok {
		c.logger.Infof("ignoring error creating bgpPeer %v: %v", peer.Name, err)
		return nil
	}
	return err
}

func (c *bgpPeerClient) Delete(name string) (err error) {
	defer func(begin time.Time, op string) {
		c.latencies.WithLabelValues(op).Observe(time.Since(begin).Seconds())
		if err != nil {
			c.errors.WithLabelValues(op).Inc()
		}
	}(time.Now(), opDelete)
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	_, err = c.calicoClient.BGPPeers().Delete(ctx, name, options.DeleteOptions{})
	if _, ok := err.(calicoerrors.ErrorResourceDoesNotExist); ok {
		c.logger.Infof("ignoring error deleting bgpPeer %v: %v", name, err)
		return nil
	}
	return err
}

func (c *bgpPeerClient) List() (list *apiv3.BGPPeerList, err error) {
	defer func(begin time.Time, op string) {
		c.latencies.WithLabelValues(op).Observe(time.Since(begin).Seconds())
		if err != nil {
			c.errors.WithLabelValues(op).Inc()
		}
	}(time.Now(), opList)
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.calicoClient.BGPPeers().List(ctx, options.ListOptions{})
}
