package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	calicotesting "github.com/tsuru/remesher/pkg/calico/testing"
)

func Test_controller_getCurrentBGPPeers(t *testing.T) {
	tests := []struct {
		name              string
		nodeName          string
		includeAllGlobals bool
		peers             []calicoapiv3.BGPPeer
		want              []calicoapiv3.BGPPeer
		wantErr           error
	}{
		{
			name:              "include-global-peer",
			nodeName:          "node1",
			includeAllGlobals: true,
			peers:             []calicoapiv3.BGPPeer{newBGPPeer("", "192.168.1.1", "", true)},
			want:              []calicoapiv3.BGPPeer{newBGPPeer("", "192.168.1.1", "", true)},
		},
		{
			name:              "ignore-global-peer",
			nodeName:          "node1",
			includeAllGlobals: false,
			peers:             []calicoapiv3.BGPPeer{newBGPPeer("", "192.168.1.1", "", true)},
			want:              []calicoapiv3.BGPPeer{},
		},
		{
			name:              "include-peers-from-node",
			nodeName:          "node1",
			includeAllGlobals: false,
			peers:             []calicoapiv3.BGPPeer{newBGPPeer("node1", "192.168.1.1", "", true), newBGPPeer("node2", "192.168.1.1", "", true)},
			want:              []calicoapiv3.BGPPeer{newBGPPeer("node1", "192.168.1.1", "", true)},
		},
		{
			name:              "include-peers-from-node-and-globals",
			nodeName:          "node1",
			includeAllGlobals: true,
			peers:             []calicoapiv3.BGPPeer{newBGPPeer("node1", "192.168.1.1", "", true), newBGPPeer("node2", "192.168.1.1", "", true), newBGPPeer("", "192.168.1.1", "", true)},
			want:              []calicoapiv3.BGPPeer{newBGPPeer("node1", "192.168.1.1", "", true), newBGPPeer("", "192.168.1.1", "", true)},
		},
		{
			name:              "include-peers-referring-node",
			nodeName:          "node1",
			includeAllGlobals: true,
			peers:             []calicoapiv3.BGPPeer{newBGPPeer("node2", "192.168.1.1", "node1", true), newBGPPeer("", "192.168.1.1", "", true)},
			want:              []calicoapiv3.BGPPeer{newBGPPeer("node2", "192.168.1.1", "node1", true), newBGPPeer("", "192.168.1.1", "", true)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calicoClient := &calicotesting.InMemoryPeerClient{}
			for i := range tt.peers {
				err := calicoClient.Create(&tt.peers[i])
				assert.NoError(t, err)
			}
			c := controller{calicoClient: calicoClient}
			got, err := c.getCurrentBGPPeers(tt.nodeName, tt.includeAllGlobals)
			assert.Equal(t, tt.wantErr, err)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}
