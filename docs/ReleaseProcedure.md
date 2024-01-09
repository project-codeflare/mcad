# Steps for making an MCAD release

## Do a PR to publish a new Helm chart.

1. Create a branch `release-prep`
2. cd deployment
3. Edit mcad-deployment/Chart.yaml to update `version` to `X.Y.Z`
4. Edit mcad-deployment/values.yaml to change `tag` from `latest` to `vX.Y.Z`
5. Execute `helm package mcad-controller`
6. Restore `tag` in values.yaml to `latest`
7. `cp mcad-controller-X.Y.Z.tgz repo`
8. Extract the previous and X.Y.Z tar balls and do a `diff -r` to verify
   that the new chart has the expected values/changes.  If something is wrong,
   fix it and generate a new chart.  Repeat until happy...
9. `helm repo index repo --url https://raw.githubusercontent.com/project-codeflare/mcad/main/deployment/repo/`
10. `git add .`
11.  Submit PR, merge it.


## Tag main branch

1. Be sure to update local clone so you have the merged PR
2. `git tag -s vX.Y.Z`
3. `git push --tags upstream`
4. Wait for the MCAD-Release workflow to complete and verify that it
   successfully pushed the `vX.Y.Z` image to quay.io/ibm/mcad

## Update `latest` tag in quay.io

1. Use the quay.io UI to move the `latest` tag to the vX.Y.Z image
