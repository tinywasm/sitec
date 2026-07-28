# Extraction pipeline

How a producer method becomes a line in the shipped stylesheet. Context in
[ARCHITECTURE.md §4](../ARCHITECTURE.md#4-extraction-model).

```mermaid
flowchart TD
    A[modfind discovers modules]
    B[expand each module<br/>to the packages inside it]
    C[parse every non-test .go file<br/>collect producer names and receiver types]
    D{any producer found?}
    E[error: package imports a styling<br/>library but declares none]
    F[generate main.go<br/>import each package, instantiate each type]
    G[go run<br/>producers execute, JSON on stdout]
    H[merge by package path<br/>sorted, deterministic]
    I[hoist the layer statement<br/>merge identical blocks]
    J[assetmin.SSRAssets]

    A --> B
    B --> C
    C --> D
    D -->|no, but imports one| E
    D -->|yes| F
    F --> G
    G --> H
    H --> I
    I --> J
```

Detection identifies **names and types only**. Correctness of the call is
delegated to the Go compiler when the generated program is built, which is why a
producer may return any type with the right shape.

## Where assets are lost today

```mermaid
flowchart TD
    A[producer declared]
    B{in css.go, js.go,<br/>svg.go or html.go?}
    C[never read<br/>and no error fires]
    D{first matching<br/>type in the package?}
    E[dropped<br/>FindSubmatch returns one]
    F{signature on<br/>one line?}
    G[no match<br/>no stylesheet]
    H[collected]

    A --> B
    B -->|no| C
    B -->|yes| D
    D -->|no| E
    D -->|yes| F
    F -->|no| G
    F -->|yes| H
```

All three loss paths end in a green build and an unstyled component. Closing them
is the subject of [SPECS.md §6](../SPECS.md#6-published-behaviour).
