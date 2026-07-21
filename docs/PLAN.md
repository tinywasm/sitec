---
PLAN: "fix: stop mergeResultsFor mutating cached sprites so module icons are not lost"
---

# PLAN — SSR icon-loss bug (cached sprite mutation)

Self-contained fix plan for `github.com/tinywasm/ssr`.

**The regression test MUST live in `tests/`** (package `ssr_test`), matching
this repo's convention — every existing test is there. Do not leave `_test.go`
files at the module root.

The reproduction is the direct unit test of `mergeResultsFor` (already written
and confirmed failing at the module root). To move it into `tests/` as
`package ssr_test`, export the symbols it needs — nothing else about them
changes:

- `mergeResultsFor` → `MergeResultsFor` (keep signature).
- `ssrCollectorOutput` → an exported type (e.g. `CollectorOutput`) with its
  `Icons *sprite.Sprite` field exported, so the test can build the input
  `map[string]CollectorOutput`. Update the internal uses (`invoke.go`,
  `cache.go`, `extract.go`) to the exported name.

Then the test asserts, over a hand-built `results` map (crudview=3, platformd=3,
targetlist=1):

1. `MergeResultsFor("app", results)` → merged `Icons.Len()` == 6.
2. `results["app/crudview"].Icons.Len()` is STILL 3 — the cached package sprite
   was not mutated.
3. A SECOND `MergeResultsFor("app", results)` → still 6 (not 9).

Before the fix: step 2 sees 6 and step 3 sees 9 (confirmed). After the fix all
three hold. Move the existing `mergeicons_test.go` into `tests/`, rename its
package to `ssr_test`, and qualify the exported calls (`ssr.MergeResultsFor`,
`ssr.CollectorOutput`).

## Symptom (as seen by a consumer)

A multi-module app (e.g. `tinywasm/layout`) serves the inline SVG sprite with
only the **root/first** package's icons. Other modules' symbols
(`icon-crud-*`, `tl-dots`, …) render blank even though:

- each module's `IconSvg()` returns the icons correctly,
- each module's CSS *is* served (so the modules are discovered and extracted),
- restarting the daemon does not fix it.

## Root cause — `extract.go`, `mergeResultsFor`

```go
var merged ssrCollectorOutput
for _, p := range paths {
    out := results[p]
    ...
    merged.Icons = merged.Icons.Merge(out.Icons)   // <-- the bug
}
```

`results` is **the exact map stored in the extraction cache** — `cache.get`
returns `entry.results` by reference (see `cache.go`), and within a single
`ExtractAll()` every module is extracted against the same module set, so they
all share one hash key and therefore the **same** `results` object. It must be
treated as read-only. It isn't:

- `merged` is a zero value, so on the first package `merged.Icons` is `nil`.
- `Sprite.Merge` returns `other` unchanged when the receiver is nil
  (`sprite.go`), so `merged.Icons` becomes the **same pointer** as the first
  cached package's sprite.
- The next package does `firstCachedSprite.Merge(next)`, which
  `append`s in place — permanently mutating the cached first package's sprite.

Consequence: cached package sprites accumulate foreign icons across
extractions. Counts duplicate on some rebuilds; once assetmin dedups symbols by
`id`, the net served set no longer matches any single module and specific
symbols (crudview/targetlist) drop out while the root's remain. This is why a
restart doesn't help — the corruption re-happens the moment the cache is
repopulated and reused across the `ExtractAll` module loop.

## Fix — give `merged` its own sprite; never alias a cached one

In `extract.go`:

1. Add the import:

   ```go
   "github.com/tinywasm/svg/sprite"
   ```

2. In `mergeResultsFor`, own the sprite before the loop and merge INTO it:

   ```go
   var merged ssrCollectorOutput
   merged.Icons = sprite.NewSprite() // our own sprite; never alias a cached one
   for _, p := range paths {
       out := results[p]
       merged.Root += out.Root
       merged.Render += out.Render
       merged.HTML += out.HTML
       merged.Scripts = append(merged.Scripts, out.Scripts...)
       merged.Icons.Merge(out.Icons) // appends into OUR sprite; out.Icons untouched
   }
   ```

`Merge` copies `other`'s icon entries into the receiver's slice; it does not
mutate `other`. With a non-nil receiver we own from the start, the cached
package sprites are never touched, so repeated extractions are stable.

## Verify

```
cd tinywasm/ssr
go test ./tests/...   # the new black-box guard must PASS after the fix
go test ./...         # full suite green
```

Then publish a new `tinywasm/ssr` tag. In the layout app the crudview `+`/`↺`
button, the search magnifier, and the targetlist `⋮` dots will render.

## Related (secondary, NOT the root cause — optional hardening)

`assetmin/svg.go` `mergeSprite` discards `Merge`'s return value and re-merges a
module's icons on every reload, so `masterSprite` can accumulate duplicate
`<symbol>` entries over a long session. Harmless for rendering (browsers use the
first symbol of an id) but wasteful. Consider deduping by id in `mergeSprite`,
or `masterSprite = masterSprite.Merge(s)` for nil-safety. Track separately — the
icon-loss bug is fully explained and fixed by the `ssr` change above.
