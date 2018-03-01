package controller

import (
	"testing"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				newBGPPeer("node1", "192.168.0.1", true),
				newBGPPeer("node1", "192.168.0.2", true),
			},
			wantToAdd: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1", true),
				newBGPPeer("node1", "192.168.0.2", true),
			},
		},
		{
			name: "all to be removed",
			current: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1", true),
				newBGPPeer("node1", "192.168.0.2", true),
			},
			wantToRemove: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1", true),
				newBGPPeer("node1", "192.168.0.2", true),
			},
		},
		{
			name: "some to add and some to remove",
			current: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1", true),
				newBGPPeer("node1", "192.168.0.2", true),
			},
			desired: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1", true),
				newBGPPeer("node1", "192.168.0.3", true),
			},
			wantToRemove: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.2", true),
			},
			wantToAdd: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.3", true),
			},
		},
		{
			name: "ignore unmanaged bgpeers",
			current: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1", true),
				newBGPPeer("node1", "192.168.0.2", true),
				newBGPPeer("node1", "192.168.0.4", false),
			},
			desired: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.1", true),
				newBGPPeer("node1", "192.168.0.3", true),
			},
			wantToRemove: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.2", true),
			},
			wantToAdd: []calicoapiv3.BGPPeer{
				newBGPPeer("node1", "192.168.0.3", true),
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

func newBGPPeer(node, peerIP string, managed bool) calicoapiv3.BGPPeer {
	peer := calicoapiv3.BGPPeer{
		Spec: calicoapiv3.BGPPeerSpec{
			Node:     node,
			PeerIP:   peerIP,
			ASNumber: asNumber,
		},
	}
	if managed {
		peer.ObjectMeta.Annotations = map[string]string{
			remesherManagedLabel: "true",
		}
	}
	return peer
}

func Test_buildPeer(t *testing.T) {
	tests := []struct {
		name string
		from *corev1.Node
		to   *corev1.Node
		want calicoapiv3.BGPPeer
	}{
		{
			name: "global",
			to: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name: "node1",
				},
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{{Address: "192.168.10.1"}},
				},
			},
			want: calicoapiv3.BGPPeer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "remesher-global-node1",
					Annotations: map[string]string{
						"remesher.tsuru.io/managed": "true",
					},
					Labels: map[string]string{
						"remesher.tsuru.io/managed": "true",
					},
				},
				Spec: calicoapiv3.BGPPeerSpec{
					PeerIP:   "192.168.10.1",
					ASNumber: asNumber,
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPeer(tt.from, tt.to)
			assert.Equal(t, tt.want, got)
		})
	}
}
