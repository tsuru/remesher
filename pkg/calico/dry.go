package calico

import (
	"encoding/json"

	apiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/sirupsen/logrus"
)

// NewDryBGPPeerClient creates a BGPPeerClient that is a no-op for creations and deletions
func NewDryBGPPeerClient(iface BGPPeerInterface, logger *logrus.Entry) BGPPeerInterface {
	return &dryBGPPeerClient{
		BGPPeerInterface: iface,
		logger:           logger,
	}
}

type dryBGPPeerClient struct {
	BGPPeerInterface
	logger *logrus.Entry
}

var _ BGPPeerInterface = &dryBGPPeerClient{}

func (c *dryBGPPeerClient) Create(peer *apiv3.BGPPeer) error {
	d, err := json.Marshal(peer.Spec)
	if err != nil {
		return err
	}
	peerData := make(map[string]interface{})
	if err := json.Unmarshal(d, &peerData); err != nil {
		return err
	}
	fields := map[string]interface{}{
		"op":        "create",
		"client":    "dry",
		"peer.name": peer.Name,
	}
	for k, v := range peerData {
		fields["peer."+k] = v
	}
	c.logger.WithFields(fields).Info()
	return nil
}

func (c *dryBGPPeerClient) Delete(name string) error {
	c.logger.WithFields(map[string]interface{}{
		"op":        "delete",
		"peer.name": name,
		"client":    "dry",
	}).Info()
	return nil
}

func (c *dryBGPPeerClient) List() (*apiv3.BGPPeerList, error) {
	return c.BGPPeerInterface.List()
}
