package controller

import (
	"testing"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/stretchr/testify/assert"
)

func Test_deltaPeers(t *testing.T) {
	tests := []struct {
		name         string
		current      []calicoapiv3.BGPPeer
		desired      []calicoapiv3.BGPPeer
		wantToAdd    []calicoapiv3.BGPPeer
		wantToRemove []calicoapiv3.BGPPeer
	}{
		{
			name: "all to be added",
			desired: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1"),
				newBGPPeer("node1", "192.168.0.2"),
			},
			wantToAdd: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1"),
				newBGPPeer("node1", "192.168.0.2"),
			},
		},
		{
			name: "all to be removed",
			current: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1"),
				newBGPPeer("node1", "192.168.0.2"),
			},
			wantToRemove: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1"),
				newBGPPeer("node1", "192.168.0.2"),
			},
		},
		{
			name: "some to add and some to remove",
			current: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1"),
				newBGPPeer("node1", "192.168.0.2"),
			},
			desired: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1"),
				newBGPPeer("node1", "192.168.0.3"),
			},
			wantToRemove: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.2"),
			},
			wantToAdd: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.3"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotToAdd, gotToRemove := deltaPeers(tt.current, tt.desired)
			assert.Equal(t, tt.wantToAdd, gotToAdd)
			assert.Equal(t, tt.wantToRemove, gotToRemove)
		})
	}
}

func newBGPPeer(node, peerIP string) calicoapiv3.BGPPeer {
	return calicoapiv3.BGPPeer{
		Spec: calicoapiv3.BGPPeerSpec{
			Node:     node,
			PeerIP:   peerIP,
			ASNumber: asNumber,
		},
	}
}
