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

export NUMBER_OF_ITERATIONS=${1-5}
export DELAY_BETWEEN_ITERATIONS=${2-20}
export LOGFILE_PATH=${3-./mcadlog.txt}
# Checking if prerequisites are installed
function CheckPrerequisites {
  echo "==> Checking prerequisites"

  #checking that pkill exists
  which pkill >/dev/null 2>&1
  if [ $? -ne 0 ]
  then
    echo "pkill not installed, exiting."
    exit 1
  fi

  #checking that kind exists
  which kind >/dev/null 2>&1
  if [ $? -ne 0 ]
  then
    echo "kind not installed, exiting."
    exit 1
  else
    echo -n "found kind, version: " && kind version
  fi

  #checking that kubectl exists
  which kubectl >/dev/null 2>&1
  if [ $? -ne 0 ]
  then
      sudo apt-get install -y --allow-unauthenticated kubectl
      [ $? -ne 0 ] && echo "Failed to install kubectl" && exit 1
      echo "kubectl was sucessfully installed."
  fi

  #checking that mcad main file exists
  ls ./cmd/main.go >/dev/null 2>&1
  if [ $? -ne 0 ]
  then
    echo "Can't find mcad main file ( under ./cmd/main.go ), exiting. "
    exit 1
  fi
}

function CreateKindCluster {
  echo "==> Creating kind cluster"
  kind create cluster --name test
  kubectl cluster-info --context kind-test
  kubectl create serviceaccount default -n default
}

function DeleteKindCluster {
  echo "==> Deleting kind cluster"

  kind get clusters | grep test >/dev/null 2>&1
  if [ $? -eq 0 ]
  then
    echo "Kind cluster found, deleting."
    kind delete cluster --name test
  fi
}

function StartMCADController {
  echo "==> Starting MCAD controller"
  echo "MCAD logs will be captured at: ${LOGFILE_PATH}"
  echo "Installaing CRDs into kube cluster"
  make install
  echo "Starting MCAD controller in the background"
  go run ./cmd/main.go --metrics-bind-address=localhost:0 --health-probe-bind-address=localhost:0 --zap-log-level=info > ${LOGFILE_PATH} 2>&1 & 
  MCAD_BACKGROUND_PID=$!
}

function StopMCADController {
  echo "==> Stopping MCAD controller"
  echo "killing process: ${MCAD_BACKGROUND_PID}"
  kill -9 ${MCAD_BACKGROUND_PID}
  pkill -P $$ --signal 9
}

function CreateAppWrapper {
  echo "==> Creating AppWrapper"
  kubectl apply -f ./hack/sample-appwrappers/appwrapper-simple-test.yaml
}

function MonitorAppWrapper {
  echo "==> Moniroting AppWrapper progress"
  for i in $(seq 1 ${DELAY_BETWEEN_ITERATIONS}); do
    kubectl get -f ./hack/sample-appwrappers/appwrapper-simple-test.yaml
    sleep 1
  done
}

function DeleteAppWrapper {
  echo "==> Deleting AppWrapper"
  kubectl delete --grace-period=0 -f ./hack/sample-appwrappers/appwrapper-simple-test.yaml
}


function MainLoop {
  echo "==> Starting Main Loop"
  echo "Will loop for ${NUMBER_OF_ITERATIONS} iterations"
  echo "The delay between iterations is ${DELAY_BETWEEN_ITERATIONS}"

  for i in $(seq 1 ${NUMBER_OF_ITERATIONS}); do
    echo "> Iteration ${i}"
    CreateAppWrapper
    MonitorAppWrapper
    DeleteAppWrapper
  done
}

CheckPrerequisites
DeleteKindCluster && CreateKindCluster
StartMCADController
MainLoop $1
StopMCADController
DeleteKindCluster


