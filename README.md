# remesher 

[![Build Status](https://travis-ci.org/tsuru/remesher.svg?branch=master)](https://travis-ci.org/tsuru/remesher)

Remesher is a Kubernetes controller that watches for nodes inside the cluster and generates BGPPeers (custom resource defined by calico) to
construct a customized, segregated Node to Node BGP mesh based on the node's labels.

## How it Works

Remesher constructs a customized node to node mesh based on a configurable neighborhood node label. The diagram below is an example of running
the controller on a Kubernetes cluster with a single master (denoted by the node with node-role.kubernetes.io/master label) with `--neighborhood-label remesher.io/group`.

![Diagram](https://raw.githubusercontent.com/tsuru/remesher/master/remesher.png)

This will create two partitioned node meshes, one for each group (blue, purple). Every node will be a BGPPeer of:

1. Nodes with the same `--neighborhood-label` value
2. Master nodes (based on the `node-role.kubernetes.io/master` label)
3. Global nodes (based on the `remesher.tsuru.io/global` label)

Remesher uses the calico client to create/list/delete BGPPeers resources.