# Active development on MCAD v2 has ended

**In 2024, the MCAD developers joined the [Kueue community](https://github.com/kubernetes-sigs/kueue).
We stopped working on MCAD as a standalone project and shifted to bringing the lessons learned
from MCAD to Kueue.  We developed a new [Kueue-compatabile version of AppWrapper](https://github.com/project-codeflare/appwrapper)
which makes MCAD's support for complex workloads and advanced fault tolerance available to users of Kueue.**

# MCAD v2

## Overview

This repository contains a reimplementation of MCAD
([multi-cluster-app-dispatcher](https://github.com/project-codeflare/multi-cluster-app-dispatcher))
using recent versions of [controller
runtime](https://github.com/kubernetes-sigs/controller-runtime) and
[kubebuilder](https://github.com/kubernetes-sigs/kubebuilder).

This reimplementation does not support quotas or dispatching to multiple
clusters yet.

See [PORTING.md](PORTING.md) for instructions on how to port AppWrappers from
MCAD to MCAD v2.

## Getting Started

You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

### Running locally against cluster

Install the CRDs into the cluster:

```sh
make install
```

 Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):
```sh
make run
```

**NOTE:** You can also run this in one step by running: `make install run`

Uninstall the CRDs:
```sh
make uninstall
```

### Running on the cluster

Build and push your image to the location specified by `IMG`:
```sh
make docker-build docker-push IMG=<image-name>:<image-tag>
```

Or build and push a multi-architecture image with:
```sh
make docker-buildx IMG=<image-name>:<image-tag>
```

Deploy the CRDs and controller to the cluster with the image specified by `IMG`:
```sh
make deploy IMG=<image-name>:<image-tag>
```

Undeploy the CRDs and controller from the cluster:
```sh
make undeploy
```

### Modifying the API definitions

If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

## Helm Chart

Alternatively, MCAD can be installed on a cluster using Helm.

Install the CRDs and controller in the `mcad-system` namespace:
```sh
helm install --namespace mcad-system mcad-controller deployment/mcad-controller \
  --create-namespace \
  --set image.repository=<image-name> \
  --set image.tag=<image-tag> \
  --set resources.requests.cpu=100m \
  --set resources.requests.memory=512Mi \
  --set resources.limits.cpu=2000m \
  --set resources.limits.memory=4096Mi
```

Uninstall from `mcad-system` namespace:
```sh
helm uninstall mcad-controller -n mcad-system
```

Uninstall CRDs:
```sh
kubectl delete crd appwrappers.workload.codeflare.dev
```

## Pre-commit hooks

This repository includes pre-configured pre-commit hooks. Make sure to install
the hooks immediately after cloning the repository:
```sh
pre-commit install
```
See [https://pre-commit.com](https://pre-commit.com) for prerequisites.

## Running tests locally

You can do `make run-e2e` to build MCAD and run the entire
test suite against it on freshly created `kind` cluster in
a fully automated fashion.  For development purposes, other
modes are also supported. See the detailed instructons in
[test/README.md](test/README.md) for more details.

## License

Copyright 2023 IBM Corporation.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
