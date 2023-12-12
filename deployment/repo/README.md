# Helm Repository

Usage:
```sh
helm repo add mcad https://raw.githubusercontent.com/project-codeflare/mcad/main/deployment/repo/

helm repo update

helm install mcad-controller mcad/mcad-controller --namespace kube-system  . \
  --set resources.requests.cpu=100m \
  --set resources.requests.memory=512Mi \
  --set resources.limits.cpu=2000m \
  --set resources.limits.memory=4096Mi
  --set image.repository=quay.io/ibm/mcad \
  --set image.tag=v2.1.0 \
```
