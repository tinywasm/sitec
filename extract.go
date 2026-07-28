package ssr

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/tinywasm/assetmin"
	"github.com/tinywasm/fmt"
	"github.com/tinywasm/js"
	"github.com/tinywasm/svg/sprite"
)

var reLayer = regexp.MustCompile(`@layer\s+([^;{]+);`)

// extractAssetsForModule is the internal implementation that takes a resolved module.
func extractAssetsForModule(m module, rootDir string, allModules []module, binCachePath string, cache *ssrCache, scanner *scanner, assetLibraries []string, log func(...any), mu *sync.Mutex) (*assetmin.SSRAssets, error) {
	// Ensure m is in the extractor's module set, so the generated main.go
	// imports it and the results map carries an entry for m.path.
	modulesForExtract := allModules
	if !containsModule(allModules, m) {
		modulesForExtract = append(append([]module(nil), allModules...), m)
	}

	// Compute hash of all modules to check global cache
	hashKey, err := computeModuleHashSet(modulesForExtract)
	if err != nil {
		return nil, fmt.Err("failed to compute module hash", err)
	}

	// Check cache
	mu.Lock()
	cachedResults, hasCached := cache.get(hashKey)
	if !hasCached {
		// Do compile-and-invoke
		results, err := invokeSSRExtractorOnce(rootDir, modulesForExtract, scanner, assetLibraries)
		if err != nil {
			mu.Unlock()
			return nil, err
		}

		// Cache the results
		cache.set(hashKey, results)
		cachedResults = results
	}
	mu.Unlock()

	// Collect the results for the requested module AND for the packages inside it.
	// The assets of an app live in its packages (config/, modules/x/), never at the
	// module root — asking only for m.path returned nothing and the app shipped with
	// an empty stylesheet.
	output, ok, err := MergeResultsFor(m.path, cachedResults)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	scripts := make([]*js.Script, 0, len(output.Scripts))
	for _, s := range output.Scripts {
		scripts = append(scripts, &js.Script{
			Name:    s.Name,
			Content: s.Content,
		})
	}

	return &assetmin.SSRAssets{
		ModuleName: m.path,
		RootCSS:    output.Root,
		CSS:        output.Render,
		JS:         scripts,
		HTML:       output.HTML,
		Icons:      output.Icons,
	}, nil
}

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

	// Step 4: Layer statement hoist
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

	var singleLayerStatement string
	if len(allLayers) > 0 {
		singleLayerStatement = allLayers[0].statement + "\n"
	}

	var merged CollectorOutput
	merged.Icons = sprite.NewSprite()

	var rawRenderCSS strings.Builder
	for _, p := range paths {
		out := results[p]
		cleanRoot := reLayer.ReplaceAllString(out.Root, "")
		cleanRender := reLayer.ReplaceAllString(out.Render, "")

		merged.Root += cleanRoot
		rawRenderCSS.WriteString(cleanRender)
		merged.HTML += out.HTML
		merged.Scripts = append(merged.Scripts, out.Scripts...)
		if out.Icons != nil {
			merged.Icons.Merge(out.Icons)
		}
	}

	// Apply Step 5: Merge identical blocks
	merged.Render = mergeCSS(rawRenderCSS.String())

	// Prepend the single layer statement if there is one
	if singleLayerStatement != "" {
		merged.Render = singleLayerStatement + merged.Render
	}

	return merged, true, nil
}

// findProjectRoot finds the project root by locating the nearest go.mod file above or at startDir.
func findProjectRoot(startDir string) (string, error) {
	dir := startDir
	for {
		gomodPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(gomodPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Err("no go.mod found in", startDir, "or parent directories")
		}
		dir = parent
	}
}

func containsModule(mods []module, m module) bool {
	for _, x := range mods {
		if x.path == m.path && x.dir == m.dir {
			return true
		}
	}
	return false
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

func findMatchingBrace(s string, start int) (int, bool) {
	depth := 0
	for i := start; i < len(s); i++ {
		if s[i] == '{' {
			depth++
		} else if s[i] == '}' {
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return -1, false
}

type CSSRule struct {
	Selector     string
	Declarations string
	Raw          string
}

func parseRules(content string) []CSSRule {
	var rules []CSSRule
	i := 0
	for i < len(content) {
		startBrace := strings.IndexByte(content[i:], '{')
		if startBrace == -1 {
			break
		}
		startBrace += i
		endBrace, ok := findMatchingBrace(content, startBrace)
		if !ok {
			break
		}
		selector := strings.TrimSpace(content[i:startBrace])
		declarations := strings.TrimSpace(content[startBrace+1 : endBrace])
		raw := content[i : endBrace+1]
		rules = append(rules, CSSRule{
			Selector:     selector,
			Declarations: declarations,
			Raw:          raw,
		})
		i = endBrace + 1
	}
	return rules
}

func getSelectors(selectorStr string) map[string]bool {
	parts := strings.Split(selectorStr, ",")
	res := make(map[string]bool)
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			res[trimmed] = true
		}
	}
	return res
}

func selectorsOverlap(s1, s2 map[string]bool) bool {
	for k := range s1 {
		if s2[k] {
			return true
		}
	}
	return false
}

func mergeRules(rules []CSSRule) []CSSRule {
	type ruleState struct {
		selectors    map[string]bool
		declarations string
		raw          string
		merged       bool
	}

	states := make([]ruleState, len(rules))
	for i, r := range rules {
		states[i] = ruleState{
			selectors:    getSelectors(r.Selector),
			declarations: r.Declarations,
			raw:          r.Raw,
			merged:       false,
		}
	}

	for i := 0; i < len(states); i++ {
		if states[i].merged {
			continue
		}
		for j := i + 1; j < len(states); j++ {
			if states[j].merged {
				continue
			}
			if states[j].declarations == states[i].declarations {
				overlap := false
				for k := i + 1; k < j; k++ {
					if states[k].merged {
						continue
					}
					if selectorsOverlap(states[k].selectors, states[i].selectors) ||
						selectorsOverlap(states[k].selectors, states[j].selectors) {
						overlap = true
						break
					}
				}
				if !overlap {
					for sel := range states[j].selectors {
						states[i].selectors[sel] = true
					}
					states[j].merged = true
				}
			}
		}
	}

	var mergedRules []CSSRule
	for _, s := range states {
		if s.merged {
			continue
		}
		var sels []string
		for sel := range s.selectors {
			sels = append(sels, sel)
		}
		sort.Strings(sels)
		combinedSelector := strings.Join(sels, ", ")
		rawRule := combinedSelector + " { " + s.declarations + " }"
		mergedRules = append(mergedRules, CSSRule{
			Selector:     combinedSelector,
			Declarations: s.declarations,
			Raw:          rawRule,
		})
	}
	return mergedRules
}

type CSSItem struct {
	Type      string
	LayerName string
	Content   string
	Rules     []CSSRule
}

func parseStylesheet(css string) []CSSItem {
	var items []CSSItem
	i := 0
	for i < len(css) {
		idx := strings.Index(css[i:], "@layer")
		if idx == -1 {
			if strings.TrimSpace(css[i:]) != "" {
				items = append(items, CSSItem{Type: "raw", Content: css[i:]})
			}
			break
		}
		idx += i
		if idx > i {
			raw := css[i:idx]
			if strings.TrimSpace(raw) != "" {
				items = append(items, CSSItem{Type: "raw", Content: raw})
			}
		}

		startBrace := strings.IndexByte(css[idx:], '{')
		if startBrace == -1 {
			items = append(items, CSSItem{Type: "raw", Content: css[idx:]})
			break
		}
		startBrace += idx

		layerHeader := css[idx+6 : startBrace]
		layerName := strings.TrimSpace(layerHeader)

		endBrace, ok := findMatchingBrace(css, startBrace)
		if !ok {
			items = append(items, CSSItem{Type: "raw", Content: css[idx:]})
			break
		}

		layerContent := css[startBrace+1 : endBrace]
		rules := parseRules(layerContent)

		items = append(items, CSSItem{
			Type:      "layer",
			LayerName: layerName,
			Rules:     rules,
		})

		i = endBrace + 1
	}
	return items
}

func mergeCSS(css string) string {
	items := parseStylesheet(css)

	firstIdx := make(map[string]int)
	layerRules := make(map[string][]CSSRule)

	for idx, item := range items {
		if item.Type == "layer" {
			if _, ok := firstIdx[item.LayerName]; !ok {
				firstIdx[item.LayerName] = idx
			}
			layerRules[item.LayerName] = append(layerRules[item.LayerName], item.Rules...)
		}
	}

	mergedLayerRules := make(map[string][]CSSRule)
	for name, rules := range layerRules {
		mergedLayerRules[name] = mergeRules(rules)
	}

	var sb strings.Builder
	for idx, item := range items {
		if item.Type == "raw" {
			sb.WriteString(item.Content)
		} else if item.Type == "layer" {
			if firstIdx[item.LayerName] == idx {
				sb.WriteString("@layer ")
				sb.WriteString(item.LayerName)
				sb.WriteString(" {\n")
				for _, r := range mergedLayerRules[item.LayerName] {
					sb.WriteString(r.Raw)
					sb.WriteString("\n")
				}
				sb.WriteString("}\n")
			}
		}
	}
	return sb.String()
}
