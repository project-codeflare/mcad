# AppWrapper State Diagram (Split Controller View)
The following state diagram describes the transitions between the states of an AppWrapper
focusing on the division of the states handled by the Dispatcher and Runner.
The placement of the box indicates which controller is responsible for handling that
state and initiating the transition to the next state.
The first row of each state indicates the `AppWrapperState` and the second indicates the `AppWrapperStep`.

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
        rc --> rd : requeueOrFail
        rcd --> rd : requeueOrFail
        rc --> fc : requeueOrFail
        rc --> fd : requeueOrFail
        rcd --> fcd : requeueOrFail
        rcd --> fd : requeueOrFail
    }
```
