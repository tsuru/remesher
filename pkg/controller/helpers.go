package controller

import (
	"strings"

	calicoapiv3 "github.com/projectcalico/libcalico-go/lib/apis/v3"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func diff(current, desired []calicoapiv3.BGPPeer) (toAdd []calicoapiv3.BGPPeer, toRemove []calicoapiv3.BGPPeer) {
	currMap := make(map[calicoapiv3.BGPPeerSpec]calicoapiv3.BGPPeer)
	for _, c := range current {
		currMap[c.Spec] = c
	}
	for _, d := range desired {
		if _, ok := currMap[d.Spec]; ok {
			delete(currMap, d.Spec)
			continue
		}
		toAdd = append(toAdd, d)
	}
	for _, c := range currMap {
		toRemove = append(toRemove, c)
	}
	return toAdd, toRemove
}

// buildPeer builds a BGPPeer using from as Node and to IP as PeerIP
// creates a global bgpPEer if from is not set
func buildPeer(from, to *corev1.Node) calicoapiv3.BGPPeer {
	//TODO: consider nodes with multiple addresses, maybe thru an annotation on the node?
	var name, node string
	if from != nil {
		name = strings.ToLower(kubeNameRegex.ReplaceAllString(from.Name+"-"+to.Name, "-"))
		node = from.Name
	} else {
		name = strings.ToLower(kubeNameRegex.ReplaceAllString("global-"+to.Name, "-"))
	}
	return calicoapiv3.BGPPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "remesher-" + name,
			Annotations: map[string]string{
				remesherManagedLabel:  "true",
				remesherPeerNodeLabel: to.Name,
			},
			Labels: map[string]string{
				remesherManagedLabel:  "true",
				remesherPeerNodeLabel: to.Name,
			},
		},
		Spec: calicoapiv3.BGPPeerSpec{
			Node:     node,
			PeerIP:   to.Status.Addresses[0].Address,
			ASNumber: asNumber,
		},
	}
}

func isGlobal(node *corev1.Node) bool {
	_, master := node.Labels[masterLabel]
	_, global := node.Labels[globalLabel]
	return master || global
}

// buildMesh builds a BGPPeers Mesh from node to toNodes
func buildMesh(node *corev1.Node, toNodes []*corev1.Node) []calicoapiv3.BGPPeer {
	var peers []calicoapiv3.BGPPeer
	if isGlobal(node) {
		peers = append(peers, buildPeer(nil, node))
	}
	for _, n := range toNodes {
		if node.Name == n.Name {
			continue
		}
		if !isGlobal(n) {
			peers = append(peers, buildPeer(node, n))
		} else {
			peers = append(peers, buildPeer(nil, n))
		}
		if !isGlobal(node) {
			peers = append(peers, buildPeer(n, node))
		}
	}
	return peers
}
