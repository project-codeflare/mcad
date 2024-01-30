#!/bin/bash

# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Installs a kueue release onto an existing cluster

export ROOT_DIR="$(dirname "$(dirname "$(readlink -fn "$0")")")"

KUEUE_VERSION=v0.5.2

kubectl apply --server-side -f https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml

# hack: wait for kueue to be ready before creating queues
sleep 15

kubectl apply -f ${ROOT_DIR}/hack/kueue-config.yaml
