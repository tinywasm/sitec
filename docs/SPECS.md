# Specification — `tinywasm/ssr`

Strict functional requirements: exact detection rules, merge semantics, error
conditions and output shape. Structure and reasoning are not repeated here — see
[ARCHITECTURE.md](ARCHITECTURE.md) and [DESIGN.md](DESIGN.md).

Every rule below is a test assertion.

---

## 1. Producer detection

A **producer** is a method or function with one of these names, regardless of
signature, receiver type or return type:

`RootCSS`, `RenderCSS`, `RenderHTML`, `RenderJS`, `IconSvg`

### 1.1 Scope of the search

| Rule | Value |
|---|---|
| Files searched | every `.go` file in the package that is not `_test.go` |
| Filename restriction | **none** |
| Parser | `go/ast`, reusing the mtime cache in `scanner.go` |
| Match criterion | declared name only — never signature, return type, or the package a returned type comes from |

### 1.2 Receivers

| Case | Behaviour |
|---|---|
| Method on `T` or `*T` | collected, instantiated as `&T{}` |
| Producers on several types in one package | **all** collected |
| Receiver split across lines | collected |
| Generic receiver `*T[X]` | collected; instantiated at its declared default, or reported as unsupported — never skipped silently |
| Plain function, no receiver | collected, called directly |

### 1.3 Ordering

Producer types within a package are invoked in **ascending type-name order**. This
is what keeps output deterministic when a package declares several.

---

## 2. Result mapping

| Producer | `assetmin.SSRAssets` field | Merge across packages |
|---|---|---|
| `RootCSS()` | `RootCSS` | string concatenation |
| `RenderCSS()` | `CSS` | string concatenation, then §4 |
| `RenderHTML()` | `HTML` | string concatenation |
| `RenderJS()` | `JS` | slice append |
| `IconSvg()` | `Icons` | `sprite.Merge` |

A module's result includes its own package **and every package beneath it**,
merged in ascending package-path order.

---

## 3. Error conditions

All of these fail the build. None may be a warning or a skip.

| Condition | Message shape |
|---|---|
| Package imports an asset-producing library and declares no producer | `ssr: package <path> imports <lib> but declares no producer; expected: func (w *T) RenderCSS() *css.Stylesheet` |
| Two packages declare different cascade-layer orders | `ssr: conflicting @layer order: <path-a> declares <a>, <path-b> declares <b>` |
| Generated program fails to compile | propagated verbatim, including stderr |
| A producer panics | propagated, naming the package and type |

"Asset-producing library" is currently `github.com/tinywasm/widget/style`. The
list is configuration, not a hardcoded constant.

---

## 4. CSS merge semantics

Applied to the concatenated `CSS` field, in this order.

### 4.1 Layer statement hoist

1. Collect every `@layer <list>;` statement.
2. If two differ, error — §3.
3. Emit exactly one, as the first line of the output.
4. Remove all other occurrences.

### 4.2 Identical-block merge

Two rules merge into one with a combined, sorted selector list when **all** hold:

1. They sit in the same `@layer` block.
2. Their declaration blocks are byte-identical.
3. No rule positioned between them targets a selector that either rule also
   targets.

The merged rule takes the position of the **first** occurrence.

If any condition fails, both rules are emitted unchanged. Rules outside any layer
are never merged.

### 4.3 Invariants

1. Merging never changes which declaration wins for any element.
2. Two runs over the same input produce byte-identical output.
3. The output contains exactly one `@layer …;` statement.

---

## 5. Caching

| Rule | Value |
|---|---|
| Key | hash over every non-test `.go` file across the module set |
| Scope | one `Extractor` instance |
| Effect | an unchanged project does not regenerate or recompile the extractor program |

---


## 7. Author contract

```go
func (w *T) RenderCSS() *css.Stylesheet
```

| Rule | Detail |
|---|---|
| Instantiation | `&T{}` — the producer must not read fields |
| Purity | same input, same bytes |
| Types per package | any number |
| Silence | declaring no producer while importing a styling library is an error |

---

## Related documents

- [ARCHITECTURE.md](ARCHITECTURE.md) — structure and constraints.
- [DESIGN.md](DESIGN.md) — why these rules and not others.
