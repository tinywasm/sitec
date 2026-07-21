package ssr

import (
	"testing"

	"github.com/tinywasm/svg"
	"github.com/tinywasm/svg/sprite"
)

// spriteWith builds a sprite carrying n distinct icons (id0..idN under prefix).
func spriteWith(prefix string, n int) *sprite.Sprite {
	defs := make([]sprite.Definition, 0, n)
	for i := 0; i < n; i++ {
		id := prefix + string(rune('a'+i))
		defs = append(defs, sprite.Define(svg.Icon(id), "0 0 16 16", sprite.Path("M0 0h16")))
	}
	return sprite.NewSprite(defs...)
}

// TestMergeResultsFor_DoesNotMutateCachedSprites reproduces the SSR icon-loss bug.
//
// mergeResultsFor aggregates every package under a module. The `results` map it
// reads from is the EXACT object stored in the extraction cache (cache.get returns
// entry.results by reference), so it must be treated as read-only. It is not:
//
//	merged.Icons = merged.Icons.Merge(out.Icons)   // extract.go:97
//
// `merged` is a zero value, so on the first package merged.Icons is nil. Sprite.Merge
// returns `other` unchanged when the receiver is nil — so merged.Icons becomes the
// SAME pointer as the first cached package's sprite. The next package then does
// firstCachedSprite.Merge(next), which appends in place, permanently mutating the
// cached first package's sprite.
//
// Effect in the daemon: the cache is shared across every ExtractAll/ExtractModule
// call (same module set => same hash key => same results map). Once a package's
// cached sprite has been aliased-and-appended, later extractions merge a corrupted
// sprite: icons duplicate on some rebuilds and, once assetmin dedups by id, the
// net served set no longer matches any single module — the crudview/targetlist
// symbols stop appearing while platformd's remain.
func TestMergeResultsFor_DoesNotMutateCachedSprites(t *testing.T) {
	// One module "app" with two packages, plus an unrelated module.
	results := map[string]ssrCollectorOutput{
		"app/crudview":   {Icons: spriteWith("cv", 3)}, // sorts first under "app"
		"app/platformd":  {Icons: spriteWith("pd", 3)},
		"lib/targetlist": {Icons: spriteWith("tl", 1)},
	}

	crudviewBefore := results["app/crudview"].Icons.Len()
	if crudviewBefore != 3 {
		t.Fatalf("setup: crudview should start with 3 icons, got %d", crudviewBefore)
	}

	// First aggregation of module "app": crudview(3) + platformd(3) = 6.
	merged1, ok := mergeResultsFor("app", results)
	if !ok {
		t.Fatal("mergeResultsFor(app) returned ok=false")
	}
	if got := merged1.Icons.Len(); got != 6 {
		t.Fatalf("first merge: want 6 icons, got %d", got)
	}

	// THE BUG: the cached crudview package sprite must not have been touched.
	if got := results["app/crudview"].Icons.Len(); got != 3 {
		t.Errorf("cached crudview sprite was mutated by mergeResultsFor: want 3, got %d "+
			"(mergeResultsFor appended into the cached sprite in place)", got)
	}

	// Second aggregation of the SAME cached map (simulates the next ExtractAll with
	// an unchanged module set => cache hit => same results object). With a correct
	// (non-mutating) merge this is 6 again; with the bug it is 9 (platformd merged
	// twice into the corrupted crudview sprite).
	merged2, _ := mergeResultsFor("app", results)
	if got := merged2.Icons.Len(); got != 6 {
		t.Errorf("second merge over cached results: want 6 icons, got %d "+
			"(cache corruption makes icon counts drift across extractions)", got)
	}
}
