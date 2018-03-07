package testing

import (
	apiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
)

// InMemoryPeerClient is a calico.BGPPeerInterface implementation that uses a in memory storage
// for testing purposes
type InMemoryPeerClient struct {
	Peers map[string]apiv3.BGPPeer
}

// Create stores the BGPPeer in the backing map
func (c *InMemoryPeerClient) Create(peer *apiv3.BGPPeer) error {
	if c.Peers == nil {
		c.Peers = make(map[string]apiv3.BGPPeer)
	}
	if _, ok := c.Peers[peer.Name]; ok {
		return nil
	}
	c.Peers[peer.Name] = *peer
	return nil
}

// Delete removes the BGPPeer from the map
func (c *InMemoryPeerClient) Delete(name string) error {
	delete(c.Peers, name)
	return nil
}

// List lists the BGPPeers in the map
func (c *InMemoryPeerClient) List() (*apiv3.BGPPeerList, error) {
	list := apiv3.NewBGPPeerList()
	for _, v := range c.Peers {
		list.Items = append(list.Items, v)
	}
	return list, nil
}
