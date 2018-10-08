package controller

import (
	"reflect"
	"sort"
	"testing"
	"time"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	"github.com/stretchr/testify/assert"
	calicotesting "github.com/tsuru/remesher/pkg/calico/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
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

func Test_controller_getBGPNeighbors(t *testing.T) {
	globalNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "global",
			Labels: map[string]string{
				globalLabel: "true",
			},
		},
	}
	masterNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "master",
			Labels: map[string]string{
				masterLabel: "true",
			},
		},
	}
	nb1 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nb1-1",
			Labels: map[string]string{
				"cluster": "1",
			},
		},
	}
	nb2 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nb2-1",
			Labels: map[string]string{
				"cluster": "2",
			},
		},
	}
	nb22 := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nb2-2",
			Labels: map[string]string{
				"cluster": "2",
			},
		},
	}
	noLabelNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "no-label",
		},
	}

	allNodes := []*corev1.Node{globalNode, masterNode, nb1, nb2, nb22, noLabelNode}

	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, time.Minute)

	nodeInformer := factory.Core().V1().Nodes()

	for _, n := range allNodes {
		err := nodeInformer.Informer().GetStore().Add(n)
		if err != nil {
			t.Errorf("failed to add node %q to store: %v", n.Name, err)
		}
	}

	c := &controller{
		nodesInformer:     nodeInformer,
		neighborhoodLabel: "cluster",
	}
	tests := []struct {
		name    string
		node    *corev1.Node
		want    []*corev1.Node
		wantErr bool
	}{
		{
			name: "global-node",
			node: globalNode,
			want: allNodes,
		},
		{
			name: "master-node",
			node: masterNode,
			want: allNodes,
		},
		{
			name: "lonely-node",
			node: nb1,
			want: []*corev1.Node{globalNode, masterNode, nb1},
		},
		{
			name: "neighborhood-node",
			node: nb2,
			want: []*corev1.Node{globalNode, masterNode, nb2, nb22},
		},
		{
			name: "without-label",
			node: noLabelNode,
			want: []*corev1.Node{globalNode, masterNode},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.getBGPNeighbors(tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("controller.getBGPNeighbors() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			sort.Slice(got, func(i, j int) bool {
				return got[i].Name < got[j].Name
			})
			sort.Slice(tt.want, func(i, j int) bool {
				return tt.want[i].Name < tt.want[j].Name
			})
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("controller.getBGPNeighbors() = %v, want %v", got, tt.want)
			}
		})
	}
}
