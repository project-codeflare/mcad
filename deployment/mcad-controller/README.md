# Helm Chart

Usage:
```sh
helm install --namespace kube-system mcad-controller . \
  --set image.repository=quay.io/tardieu/micromcad \
  --set image.tag=latest \
  --set resources.requests.cpu=100m \
  --set resources.requests.memory=512Mi
```
