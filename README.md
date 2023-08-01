# MCAD

MCAD implementation using a reconciler.

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
make docker-build docker-push IMG=<some-registry>/mcad:<some-tag>
```

Deploy the CRDs and controller to the cluster with the image specified by `IMG`:
```sh
make deploy IMG=<some-registry>/mcad:<some-tag>
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

## License

Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

