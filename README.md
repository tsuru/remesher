# remesher 

[![Build Status](https://travis-ci.org/tsuru/remesher.svg?branch=master)](https://travis-ci.org/tsuru/remesher)

Remesher is a Kubernetes controller that watches for nodes inside the cluster and generates BGPPeers (custom resource defined by calico) to
construct a customized, segregated Node to Node BGP mesh based on the node's labels.

## How it Works

![Diagram](https://raw.githubusercontent.com/tsuru/remesher/master/remesher.png)



