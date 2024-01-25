# Helm Chart

Usage:
```sh
helm install --namespace kube-system mcad-controller . \
  --set image.repository=quay.io/ibm/mcad \
  --set image.tag=latest \
  --set resources.requests.cpu=100m \
  --set resources.requests.memory=512Mi \
  --set resources.limits.cpu=2000m \
  --set resources.limits.memory=4096Mi
```
