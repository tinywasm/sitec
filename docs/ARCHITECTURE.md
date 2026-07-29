# Architecture of `tinywasm/ssr`

Defines the **what** and **why** of asset extraction. Abstract structure only —
exact detection rules, merge semantics and error conditions are in
[SPECS.md](SPECS.md); the reasoning and rejected alternatives are in
[DESIGN.md](DESIGN.md).

---

## 1. What `tinywasm/ssr` is

The build-time extractor. It finds every package in a project that produces
assets, runs their producers, and hands the results to `assetmin` as one set per
module.

It exists so that a component author writes **only** the component. There is no
registry to update, no init function, no manifest — a package that declares a
producer is collected because it declares one.

Module discovery is delegated to `tinywasm/modfind`, so lookups are shared and
cached across `tinywasm` tools.

---

## 2. Position in the suite

| Module | Owns | Never does |
|---|---|---|
| `tinywasm/css` | **Values** — the token catalog, light/dark switching, contrast guarantees | Know anything about components |
| `tinywasm/widget` | **Decisions** — which token applies to which part in which state | Invent a value |
| `tinywasm/ssr` | **Delivery** — collect the sheets actually used, order and deduplicate them | Know what a widget is |

See [diagrams/EXTRACTION.md](diagrams/EXTRACTION.md).

`ssr` knows nothing about widgets, surfaces or layers. It knows that some
packages produce strings of CSS and that those strings must arrive at `assetmin`
complete, ordered, and without redundancy.

**Deduplication belongs here and nowhere else.** A stylesheet is built from one
component's declarations and cannot know how many other components exist, so it
cannot decide what is redundant. `ssr` merges all of them and is the only layer
that sees the duplication.

---

## 3. The producer contract

A package is collected if it declares at least one method with one of these
names, on any type, in any non-test file.

| Method | Result field | Meaning |
|---|---|---|
| `RootCSS()` | `RootCSS` | `:root` token declarations |
| `RenderCSS()` | `CSS` | component CSS, scoped to the component |
| `RenderHTML()` | `HTML` | prerendered markup |
| `RenderJS()` | `JS` | scripts |
| `IconSvg()` | `Icons` | sprite, merged across packages |

Obligations on the author:

1. **Zero-value instantiation.** Producers run on `&T{}`. A producer must not
   read fields.
2. **Purity.** Same input, same bytes. Extraction is cached by content hash and
   its output is committed downstream.
3. **Any number of types per package** may declare producers.
4. **Declaring none while importing an asset-producing library is an error**, not
   a skip.

### 3.1 Detection matches the method name only

Never the signature, never the return type, never the package that type comes
from. This is what allowed `IconSvg()` to change its return type without touching
the extractor, and it is why the generated program can type the sprite as `any`:
that program is compiled against whatever the target package actually returns.

Preserving this property is a hard constraint on any change to detection.

---

## 4. Extraction model

`ssr` does not parse Go to evaluate producers — it **compiles and runs them**. A
program is generated that imports every collected package, instantiates each
producer type, calls its methods, and encodes the results as JSON on stdout.

This is why detection needs only to identify *names and types*, and why producers
may return any type with the right shape: correctness is delegated to the Go
compiler rather than reimplemented.

Results are cached by a content hash over every non-test `.go` file in the module
set, so an unchanged project does not recompile.

---

## 5. Merge and ordering guarantees

1. **A module's assets include its subpackages.** Producers live in
   `config/`, `modules/x/` — rarely at the module root, which is usually
   `package main` and cannot be imported by a generated program.
2. **Stable order.** Packages are merged in sorted path order, so emitted CSS does
   not shuffle between runs.
3. **One cascade-layer statement.** The merged output declares layer order once,
   before any rule. Two packages declaring different layer orders is an error: the
   cascade of the whole application depends on it.
4. **No redundant rules.** Byte-identical declaration blocks within the same layer
   are merged into one rule with a combined selector list, preserving the position
   of the first occurrence.

---

## 6. Failure posture

**An asset that should have been collected and was not is a defect, not a
warning.** A missing stylesheet produces a component that renders unstyled while
the build stays green — the most expensive failure this module can have, because
nothing reports it and the symptom appears far from the cause.

Every detection gap is therefore resolved in the same direction: make the
extractor find it, or fail the build naming the package. Never skip quietly.

---

## Related documents

- [SPECS.md](SPECS.md) — exact detection, merge and error behaviour.
- [DESIGN.md](DESIGN.md) — why, and what was rejected.
- [diagrams/EXTRACTION.md](diagrams/EXTRACTION.md) — the pipeline.
