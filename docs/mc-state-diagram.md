# AppWrapper State Diagram (Split Controller View)
During its lifetime an AppWrapper instance will transition between a number of states.
As is typical in Kubernetes resources, the current state of an AppWrapper is encoded by the
values stored in multiple fields of its Status.

The state diagram below depicts these transitions and the division of
responsibility between our two controllers: the Dispatcher and Runner.
The placement of each box indicates which controller is responsible
for reconciling AppWrappers in that state and determining when its
Status should be updated to initiated a transition to another state.
The first row in each box indicates the `AppWrapperState` and the
second indicates the `AppWrapperStep`.

```mermaid
stateDiagram-v2
    %% Empty
    e : Empty

    %% Queued
    qi : Pending
    qi : Idle

    %% Running
    ri : Running
    ri : Dispatching
    ra : Running
    ra : Accepting
    rc : Running
    rc : Creating
    rcd : Running
    rcd : Created
    rd : Running
    rd : Deleting
    rf : Running
    rf : Deleted

    %% Succeeded
    si : Completed
    si : Idle

    %% Failed
    fc : Failed
    fc : Creating
    fcd : Failed
    fcd : Created
    fd : Failed
    fd : Deleting
    ff : Failed
    ff : Deleted
    fi: Failed
    fi : Idle

    Dispatcher : Dispatcher (Hub Cluster)
    state Dispatcher  {
        e --> qi
        qi --> ri
        ri --> ra : create BindingPolicy
        rd --> rf
        rf --> qi : delete BindingPolicy
        fd --> ff
        ff --> fi : delete BindingPolicy
    }

    Runner : Runner (Spoke Cluster)
    state Runner {
        ra --> rc
        rc --> rcd
        rcd --> si
        rc --> rd
        rcd --> rd
        rc --> fc
        rc --> fd
        rcd --> fcd
        rcd --> fd
    }
```
