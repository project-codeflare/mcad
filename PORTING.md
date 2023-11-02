# Porting AppWrappers to MicroMCAD

Using MicroMCAD requires a couple of changes to AppWrapper yamls described
below. MicroMCAD comes with a revised status and enhanced fault-tolerance capabilities.

## Required changes

Recent versions of MCAD have introduced some important changes to the AppWrapper
CRD. MicroMCAD follows suit.

First update the `apiVersion` from:
```yaml
apiVersion: mcad.ibm.com/v1beta1
kind: AppWrapper
```
to:
```yaml
apiVersion: workload.codeflare.dev/v1beta1
kind: AppWrapper
```
Second add a namespace label to wrapped resources and pod specs. Concretely,
replace every instance of:
```yaml
  labels:
    appwrapper.mcad.ibm.com: my-appwrapper-name
```
with:
```yaml
  labels:
    appwrapper.mcad.ibm.com/namespace: my-appwrapper-namespace
    appwrapper.mcad.ibm.com: my-appwrapper-name
```

Here is a complete example:
```yaml
apiVersion: workload.codeflare.dev/v1beta1 # new apiVersion
kind: AppWrapper
metadata:
  namespace: default
  name: appwrapper-sample
spec:
  priority: 5
  schedulingSpec:
    minAvailable: 1
  resources:
    GenericItems:
    - replicas: 1
      completionstatus: Complete
      custompodresources:
      - requests:
          memory: 10Mi
          cpu: 10m
        replicas: 1
      generictemplate:
        apiVersion: batch/v1
        kind: Job
        metadata:
          namespace: default
          name: sample-job
          labels:
            appwrapper.mcad.ibm.com/namespace: default # new namespace label
            appwrapper.mcad.ibm.com: appwrapper-sample
        spec:
          parallelism: 1
          completions: 1
          template:
            metadata:
              namespace: default
              labels:
                appwrapper.mcad.ibm.com/namespace: default # new namespace label
                appwrapper.mcad.ibm.com: appwrapper-sample
            spec:
              restartPolicy: Never
              containers:
                - name: busybox
                  image: busybox
                  imagePullPolicy: IfNotPresent
                  command: ["sh", "-c", "sleep 10"]
                  resources:
                    requests:
                      memory: 10Mi
                      cpu: 10m
```

## Changes to AppWrapper status

MicroMCAD reports the status of an AppWrapper as follows:
```yaml
status:
  dispatchTimestamp: "2023-11-02T15:19:09Z"
  restarts: 0
  state: Running
  step: created
  transitionCount: 3
  transitions:
  - state: Pending
    time: "2023-11-02T15:19:09Z"
  - state: Running
    step: creating
    time: "2023-11-02T15:19:09Z"
  - state: Running
    step: created
    time: "2023-11-02T15:19:09Z"
```

The `state` of the AppWrapper is either `Pending`, `Running`, `Completed`, or
`Failed`.

In the `Running` state, the `step` is either `creating`, `created`, or
`deleting` reflecting whether the wrapped resources are being deployed, running,
or being undeployed.

The `transitions` reflect the last 20 state/step changes in order and the
respective timestamps for these changes.

Here is a longer example illustrating MicroMCAD requeuing capabilities:
```yaml
status:
  dispatchTimestamp: "2023-11-02T15:32:32Z"
  requeueTimestamp: "2023-11-02T15:32:32Z"
  restarts: 1
  state: Completed
  transitionCount: 8
  transitions:
  - state: Pending
    time: "2023-11-02T15:27:32Z"
  - state: Running
    step: creating
    time: "2023-11-02T15:27:32Z"
  - state: Running
    step: created
    time: "2023-11-02T15:27:32Z"
  - reason: expected pods 2 but found pods 1
    state: Running
    step: deleting
    time: "2023-11-02T15:32:32Z"
  - state: Pending
    time: "2023-11-02T15:32:32Z"
  - state: Running
    step: creating
    time: "2023-11-02T15:32:32Z"
  - state: Running
    step: created
    time: "2023-11-02T15:32:32Z"
  - state: Completed
    time: "2023-11-02T15:32:44Z"
```
In this example, the `restarts` field reports `1` restart. The reported reason
for the restart is ` expected pods 2 but found pods 1`. This restart happened
about five minutes after dispatch reflecting the default 300s grace period
(`timeInSeconds`).

The top-level `dispatchTimestamp` and `requeueTimestamp` respectively report the
most recent dispatch and requeue times.

The AppWrapper status in MicroMCAD is still a work in progress.

## Fault-tolerance enhancements

MicroMCAD supports an extended `schedulingSpec`:

```yaml
apiVersion: workload.codeflare.dev/v1beta1
kind: AppWrapper
metadata:
  name: appwrapper-sample
spec:
  priority: 5
  schedulingSpec:
    minAvailable: 2                    # expected number of pods
    requeuing:
      maxNumRequeuings: 5              # max number of retries upon failure
      timeInSeconds: 300               # how long to wait after dispatch before checking pod counts
      forceDeletionTimeInSeconds: 120  # how long to wait before force deletion on requeuing or failure
      pauseTimeInSeconds: 300          # how long to wait before redispatching a requeued AppWrapper
  resources:
    GenericItems:
      ...
```

To request MicroMCAD to monitor the health of a running AppWrapper, a non-zero
`minAvailable` number of pods must be specified. If this field is zero or left
out from the `schedulingSpec`, MicroMCAD does not monitor the AppWrapper once
dispatched and does not reclaim resources from failed AppWrappers. In practice,
only specify `minAvailable: 0` for debugging purposes.

If `minAvailable` is greater than zero, MicroMCAD checks that the number of
running or successful pods remains equal to or greater than `minAvailable`. This
checking starts only `timeInSeconds` after dispatch to account for, e.g., large
image pulls. The default `timeInSeconds` value is `300`.

If the number of running or successful pods dips below `minAvailable` pods after
`timeInSeconds`, MicroMCAD attempts to requeue the AppWrapper by deleting the
wrapped resources. If `forceDeletionTimeInSeconds` is set to a value greater
than zero, MicroMCAD will force delete resources and pods after
`forceDeletionTimeInSeconds` if necessary. For deletion is disabled by default.

Once the failed AppWrapper is successfully requeued, i.e., after deletion or
force deletion, MicroMCAD will wait at least `pauseTimeInSeconds` before
attempting to dispatch the AppWrapper again, if specified.

If `maxNumRequeuings` is specified and greater than zero, MicroMCAD will attempt
to redispatch up to `maxNumRequeuings` times only.
