package calico

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	apiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	calicoerrors "github.com/projectcalico/libcalico-go/lib/errors"
	"github.com/projectcalico/libcalico-go/lib/options"
)

// BGPPeerInterface is a minimal interface that describe the basic operations performed by a BGPPeerClient
type BGPPeerInterface interface {
	Create(peer *apiv3.BGPPeer) error
	Delete(name string) error
	List() (*apiv3.BGPPeerList, error)
}

// BGPPeerClient is a BGPPeerInterface implements automatic timeouts
type bgpPeerClient struct {
	calicoClient calicoclientv3.Interface
	logger       *logrus.Entry

	timeout time.Duration
}

func NewBGPPeerClient(logger *logrus.Entry, timeout time.Duration) (BGPPeerInterface, error) {
	calicoClient, err := calicoclientv3.NewFromEnv()
	if err != nil {
		return nil, err
	}
	return &bgpPeerClient{
		calicoClient: calicoClient,
		timeout:      timeout,
	}, nil
}

func (c *bgpPeerClient) Create(peer *apiv3.BGPPeer) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	_, err := c.calicoClient.BGPPeers().Create(ctx, peer, options.SetOptions{})
	if _, ok := err.(calicoerrors.ErrorResourceAlreadyExists); ok {
		c.logger.Infof("ignoring error creating bgpPeer %v: %v", peer.Name, err)
		return nil
	}
	return err
}

func (c *bgpPeerClient) Delete(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	_, err := c.calicoClient.BGPPeers().Delete(ctx, name, options.DeleteOptions{})
	if _, ok := err.(calicoerrors.ErrorResourceDoesNotExist); ok {
		c.logger.Infof("ignoring error deleting bgpPeer %v: %v", name, err)
		return nil
	}
	return err
}

func (c *bgpPeerClient) List() (*apiv3.BGPPeerList, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()
	return c.calicoClient.BGPPeers().List(ctx, options.ListOptions{})
}
