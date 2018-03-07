package calico

import (
	"context"

	apiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	calicoclientv3 "github.com/projectcalico/libcalico-go/lib/clientv3"
	"github.com/projectcalico/libcalico-go/lib/options"
	"github.com/projectcalico/libcalico-go/lib/watch"
)

var _ calicoclientv3.BGPPeerInterface = &BGPPeerClient{}

// BGPPeerClient is a wrapper around the calicoclientv3.BGPPeerInterface that provides metrics
type BGPPeerClient struct {
	calicoClient calicoclientv3.Interface
}

func NewBGPPeerClient() (*BGPPeerClient, error) {
	calicoClient, err := calicoclientv3.NewFromEnv()
	if err != nil {
		return nil, err
	}
	return &BGPPeerClient{
		calicoClient: calicoClient,
	}, nil
}

func (c *BGPPeerClient) Create(ctx context.Context, res *apiv3.BGPPeer, opts options.SetOptions) (*apiv3.BGPPeer, error) {
	return c.calicoClient.BGPPeers().Create(ctx, res, opts)
}

func (c *BGPPeerClient) Update(ctx context.Context, res *apiv3.BGPPeer, opts options.SetOptions) (*apiv3.BGPPeer, error) {
	return c.calicoClient.BGPPeers().Update(ctx, res, opts)
}

func (c *BGPPeerClient) Delete(ctx context.Context, name string, opts options.DeleteOptions) (*apiv3.BGPPeer, error) {
	return c.calicoClient.BGPPeers().Delete(ctx, name, opts)
}

func (c *BGPPeerClient) Get(ctx context.Context, name string, opts options.GetOptions) (*apiv3.BGPPeer, error) {
	return c.calicoClient.BGPPeers().Get(ctx, name, opts)
}

func (c *BGPPeerClient) List(ctx context.Context, opts options.ListOptions) (*apiv3.BGPPeerList, error) {
	return c.calicoClient.BGPPeers().List(ctx, opts)
}

func (c *BGPPeerClient) Watch(ctx context.Context, opts options.ListOptions) (watch.Interface, error) {
	return c.calicoClient.BGPPeers().Watch(ctx, opts)
}
