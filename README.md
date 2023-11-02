# MicroMCAD

This repository contains a prototype MCAD implementation
([multi-cluster-app-dispatcher](https://github.com/project-codeflare/multi-cluster-app-dispatcher))
using recent versions of [controller
runtime](https://github.com/kubernetes-sigs/controller-runtime) and
[kubebuilder](https://github.com/kubernetes-sigs/kubebuilder). This prototype
does not implement quotas or dispatching to multiple clusters.

See [PORTING.md](PORTING.md) for instructions on how to port AppWrappers from
MCAD to MicroMCAD.

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

Install the CRDs and controller in the `kube-system` namespace:
```sh
helm install --namespace kube-system mcad-controller deployment/mcad-controller \
  --set image.repository=<image-name> \
  --set image.tag=<image-tag> \
  --set resources.requests.cpu=100m \
  --set resources.requests.memory=512Mi \
  --set resources.limits.cpu=2000m \
  --set resources.limits.memory=4096Mi
```

Uninstall from `kube-system` namespace:
```sh
helm uninstall mcad-controller -n kube-system
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

Make sure Kind and Helm v3 are installed on your laptop. To run kuttl tests locally use command:

```sh
sh hack/run-e2e-kind.sh <image-name> <image-tag>
```

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
