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

export ROOT_DIR="$(dirname "$(dirname "$(readlink -fn "$0")")")"
export IMAGE_REPOSITORY_MCAD="${1}"
export IMAGE_TAG_MCAD="${2}"
export MCAD_IMAGE_PULL_POLICY="${3-Always}"
export IMAGE_MCAD="${IMAGE_REPOSITORY_MCAD}:${IMAGE_TAG_MCAD}"
export GORACE=1
export CLUSTER_STARTED="false"
export KUTTL_TEST_SUITES=("${ROOT_DIR}/test/e2e-kuttl.yaml" "${ROOT_DIR}/test/e2e-kuttl-acct.yaml")

source ${ROOT_DIR}/hack/e2e-util.sh

trap cleanup EXIT

# Prepare testing environment
update_test_host
check_prerequisites
pull_images

# Create and configure test cluster
kind_up_cluster
add_virtual_GPUs
configure_cluster

# Install MCAD
install_mcad

# Run tests
run_kuttl_test_suite
go run github.com/onsi/ginkgo/v2/ginkgo -v -fail-fast --procs 1 -timeout 130m ./test/e2e

RC=$?
if [ ${RC} -eq 0 ]
then
  DUMP_LOGS="false"
fi
echo "End to end test script return code set to ${RC}"
exit ${RC}
