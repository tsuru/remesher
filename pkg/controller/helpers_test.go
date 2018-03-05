package controller

import (
	"testing"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_diff(t *testing.T) {
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotToAdd, gotToRemove := diff(tt.current, tt.desired)
			assert.ElementsMatch(t, tt.wantToAdd, gotToAdd)
			assert.ElementsMatch(t, tt.wantToRemove, gotToRemove)
		})
	}
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
			to:   newNode("node1", "192.168.10.1", nil),
			want: calicoapiv3.BGPPeer{
				ObjectMeta: metav1.ObjectMeta{
					Name: "remesher-global-node1",
					Annotations: map[string]string{
						"remesher.tsuru.io/managed":   "true",
						"remesher.tsuru.io/peer-node": "node1",
					},
					Labels: map[string]string{
						"remesher.tsuru.io/managed":   "true",
						"remesher.tsuru.io/peer-node": "node1",
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

func Test_buildMesh(t *testing.T) {
	tests := []struct {
		name    string
		node    *corev1.Node
		toNodes []*corev1.Node
		want    []calicoapiv3.BGPPeer
	}{

		{
			name: "master-global-without-neightbors",
			node: newNode("node1", "192.168.10.1", map[string]string{masterLabel: "true"}),
			want: []calicoapiv3.BGPPeer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "remesher-global-node1",
						Annotations: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
						Labels: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
					},
					Spec: calicoapiv3.BGPPeerSpec{
						PeerIP:   "192.168.10.1",
						ASNumber: asNumber,
					},
				},
			},
		},
		{
			name: "label-global-without-neightbors",
			node: newNode("node1", "192.168.10.1", map[string]string{globalLabel: "true"}),
			want: []calicoapiv3.BGPPeer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "remesher-global-node1",
						Annotations: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
						Labels: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
					},
					Spec: calicoapiv3.BGPPeerSpec{
						PeerIP:   "192.168.10.1",
						ASNumber: asNumber,
					},
				},
			},
		},
		{
			name:    "regular-with-neighbors",
			node:    newNode("node1", "192.168.10.1", nil),
			toNodes: []*corev1.Node{newNode("node2", "192.168.10.2", nil)},
			want: []calicoapiv3.BGPPeer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "remesher-node1-node2",
						Annotations: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node2",
						},
						Labels: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node2",
						},
					},
					Spec: calicoapiv3.BGPPeerSpec{
						Node:     "node1",
						PeerIP:   "192.168.10.2",
						ASNumber: asNumber,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "remesher-node2-node1",
						Annotations: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
						Labels: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
					},
					Spec: calicoapiv3.BGPPeerSpec{
						Node:     "node2",
						PeerIP:   "192.168.10.1",
						ASNumber: asNumber,
					},
				},
			},
		},
		{
			name:    "global-with-neighbors",
			node:    newNode("node1", "192.168.10.1", map[string]string{masterLabel: "true"}),
			toNodes: []*corev1.Node{newNode("node2", "192.168.10.2", nil)},
			want: []calicoapiv3.BGPPeer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "remesher-global-node1",
						Annotations: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
						Labels: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
					},
					Spec: calicoapiv3.BGPPeerSpec{
						PeerIP:   "192.168.10.1",
						ASNumber: asNumber,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "remesher-node1-node2",
						Annotations: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node2",
						},
						Labels: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node2",
						},
					},
					Spec: calicoapiv3.BGPPeerSpec{
						Node:     "node1",
						PeerIP:   "192.168.10.2",
						ASNumber: asNumber,
					},
				},
			},
		},
		{
			name:    "with-global-neighbors",
			node:    newNode("node1", "192.168.10.1", nil),
			toNodes: []*corev1.Node{newNode("node2", "192.168.10.2", map[string]string{masterLabel: "true"})},
			want: []calicoapiv3.BGPPeer{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "remesher-global-node2",
						Annotations: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node2",
						},
						Labels: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node2",
						},
					},
					Spec: calicoapiv3.BGPPeerSpec{
						PeerIP:   "192.168.10.2",
						ASNumber: asNumber,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "remesher-node2-node1",
						Annotations: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
						Labels: map[string]string{
							"remesher.tsuru.io/managed":   "true",
							"remesher.tsuru.io/peer-node": "node1",
						},
					},
					Spec: calicoapiv3.BGPPeerSpec{
						Node:     "node2",
						PeerIP:   "192.168.10.1",
						ASNumber: asNumber,
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildMesh(tt.node, tt.toNodes)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func newNode(name, address string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
		Status: corev1.NodeStatus{Addresses: []corev1.NodeAddress{{Address: address}}},
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
