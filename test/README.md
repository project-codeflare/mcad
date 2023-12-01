# MCAD End-to-End  Tests

This directory contains both go and kuttl tests suites that are
designed to be run against an MCAD operator deployed on a Kubernetes
cluster. Some tests expect the cluster to have specific resource types
available (eg `nvida.com/gpu`).

The [../hack/](../hack) directory contains scripts the can be used to
create an appropriately configured test cluster using `kind` and to run
the tests.  The tests can be run in two primary modes:
  1. ***Fully automated***  The script [../hack/run-e2e.sh](../hack/run-e2e-sh)
    fully automates creating a properly configured `kind` cluster, running all
    tests on the cluster, and then deleting the cluster when the tests are completed.
  2. ***Development mode*** The script [../hack/create-test-cluster.sh](../hack/create-test-cluster.sh)
     can be used to create a correctly configured test cluster without MCAD installed.  You can
     then use either `make install; make run` or `make install; make kind-push; make deploy` to deploy
     MCAD onto the cluster. You can then either run test cases individually or use the script
     [../hack/run-tests-on-cluster.sh](../hack/run-tests-on-cluster.sh) to run the entire test suite
     against your manually deployed MCAD.
