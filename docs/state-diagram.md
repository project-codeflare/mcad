# AppWrapper State Diagram
The following state diagram describes the transitions between the states of an AppWrapper.
The first row of each state indicates the `AppWrapperState` and the second indicates the `AppWrapperStep`.

```mermaid
stateDiagram-v2
    %% Empty
    e : Empty

    %% Queued
    qi : Pending
    qi : Idle

    %% Running
    rc : Running
    rc : Creating
    rcd : Running
    rcd : Created
    rd : Running
    rd : Deleting

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
    fi: Failed
    fi : Idle

    HappyPath : Happy Path
    state HappyPath  {
        e --> qi
        qi --> rc
        rc --> rcd
        rcd --> si
        rc --> rd : requeueOrFail
        rcd --> rd : requeueOrFail
        rd --> qi
    }
    rc --> fc : requeueOrFail
    rc --> fd : requeueOrFail
    rcd --> fcd : requeueOrFail
    rcd --> fd : requeueOrFail
    fd --> fi

    classDef failed fill:pink
    class fi failed
    class fc failed
    class fcd failed
    class fd failed

    classDef succeeded fill:lightgreen
    class si succeeded
```
