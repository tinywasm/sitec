package sitec

import (
	"regexp"
	"sort"
	"strings"

	"github.com/tinywasm/fmt"
	"github.com/tinywasm/svg/sprite"
)

var reLayer = regexp.MustCompile(`@layer\s+([^;{]+);`)

// MergeResultsFor gathers the module's own assets plus those of every package under
// it, in a stable order so the emitted CSS does not shuffle between runs.
func MergeResultsFor(modulePath string, results map[string]CollectorOutput) (CollectorOutput, bool, error) {
	paths := make([]string, 0, len(results))
	for p := range results {
		if p == modulePath || strings.HasPrefix(p, modulePath+"/") {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return CollectorOutput{}, false, nil
	}
	sort.Strings(paths)

	// Step 4: Layer statement conflict check
	var allLayers []layerInfo
	for _, p := range paths {
		out := results[p]
		allLayers = append(allLayers, extractLayers(out.Root, p)...)
		allLayers = append(allLayers, extractLayers(out.Render, p)...)
	}

	if len(allLayers) > 0 {
		first := allLayers[0]
		for i := 1; i < len(allLayers); i++ {
			current := allLayers[i]
			if !layersEqual(first.layers, current.layers) {
				return CollectorOutput{}, false, fmt.Err("ssr: conflicting @layer order:",
					fmt.Sprintf("%s declares %s, %s declares %s", first.pkgPath, strings.TrimSuffix(first.statement, ";"), current.pkgPath, strings.TrimSuffix(current.statement, ";")))
			}
		}
	}

	var merged CollectorOutput
	merged.Icons = sprite.NewSprite()
	var fontsFrom string
	var siteFrom string

	for _, p := range paths {
		out := results[p]
		merged.Root += out.Root
		merged.Render += out.Render
		merged.HTML += out.HTML
		merged.Scripts = append(merged.Scripts, out.Scripts...)
		merged.Pages = append(merged.Pages, out.Pages...)
		if out.Icons != nil {
			// Merge does not mutate the receiver — it returns a fresh sprite,
			// deliberately, so a cached per-package sprite can never be
			// aliased and corrupted. Dropping the result silently threw away
			// every icon in the ecosystem: the sprite shipped empty and each
			// <use href="#…"> in the markup pointed at a symbol that was
			// never emitted.
			merged.Icons = sprite.MergeAll(merged.Icons, out.Icons)
		}
		if out.Fonts.Family() != "" {
			if fontsFrom != "" {
				return CollectorOutput{}, false, fmt.Err("ssr: multiple Fonts() declarations:",
					fontsFrom, "and", p, "— only one package per module may declare Fonts()")
			}
			merged.Fonts = out.Fonts
			fontsFrom = p
		}
		if out.Site != nil {
			if siteFrom != "" {
				return CollectorOutput{}, false, fmt.Err("ssr: multiple RenderSite() declarations:",
					siteFrom, "and", p, "— only one package per module may declare RenderSite()")
			}
			merged.Site = out.Site
			siteFrom = p
		}
	}

	return merged, true, nil
}

type layerInfo struct {
	pkgPath   string
	statement string
	layers    []string
}

func extractLayers(css string, pkgPath string) []layerInfo {
	matches := reLayer.FindAllStringSubmatch(css, -1)
	var infos []layerInfo
	for _, m := range matches {
		statement := m[0]
		layersRaw := m[1]
		layers := parseLayerList(layersRaw)
		infos = append(infos, layerInfo{
			pkgPath:   pkgPath,
			statement: statement,
			layers:    layers,
		})
	}
	return infos
}

func parseLayerList(s string) []string {
	parts := strings.Split(s, ",")
	var list []string
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			list = append(list, trimmed)
		}
	}
	return list
}

func layersEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
