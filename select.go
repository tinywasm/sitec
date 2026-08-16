package sitec

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func findProjectRoot(startDir string) (string, error) {
	dir := startDir
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found in %s or parent directories", startDir)
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

func expandToSSRPackages(modules []module, scanner *scanner, assetLibraries []string) []module {
	var out []module
	seen := make(map[string]bool)

	for _, m := range modules {
		if m.dir == "" {
			if !seen[m.path] {
				seen[m.path] = true
				out = append(out, m)
			}
			continue
		}

		filepath.WalkDir(m.dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil
			}
			if path != m.dir {
				name := d.Name()
				if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
					name == "vendor" || name == "testdata" || name == "node_modules" {
					return filepath.SkipDir
				}
				// A nested go.mod is its own module: it comes from the finder, not from here.
				if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}

			feats, err := scanner.scanPackage(path)
			if err != nil {
				return nil
			}

			// Un package main no se puede importar, así que nunca puede aportar assets.
			if feats.PkgName == mainPackageName {
				return nil // no seleccionar; seguir bajando a subdirectorios
			}

			hasProducers := len(feats.Producers) > 0

			var hasCSSGo bool
			if _, err := os.Stat(filepath.Join(path, cssSourceFile)); err == nil {
				hasCSSGo = true
			}

			var importedLib string
			if !hasProducers {
				for imp := range feats.Imports {
					for _, lib := range assetLibraries {
						if imp == lib || strings.HasSuffix(imp, "/"+lib) {
							importedLib = imp
							break
						}
					}
					if importedLib != "" {
						break
					}
				}
			}

			if hasProducers || hasCSSGo || importedLib != "" {
				pkgPath := m.path
				if rel, err := filepath.Rel(m.dir, path); err == nil && rel != "." {
					pkgPath = m.path + "/" + filepath.ToSlash(rel)
				}
				if !seen[pkgPath] {
					seen[pkgPath] = true
					out = append(out, module{path: pkgPath, dir: path})
				}
			}
			return nil
		})
	}
	return out
}

func modulesToAliases(modules []module, scanner *scanner, assetLibraries []string, rootDir string, lister GraphLister, log func(...any)) ([]moduleAlias, error) {
	reach := computeReachability(rootDir, lister)

	var skippedCount int
	var aliases []moduleAlias
	for _, m := range expandToSSRPackages(modules, scanner, assetLibraries) {
		if reach.known && !reach.set[m.path] {
			skippedCount++
			continue
		}

		parts := strings.Split(m.path, "/")
		alias := strings.ReplaceAll(parts[len(parts)-1], "-", "_")
		alias = aliasPrefix + alias

		ma := moduleAlias{
			Path:  m.path,
			Alias: alias,
		}

		if m.dir != "" {
			feats, err := scanner.scanPackage(m.dir)
			if err != nil {
				return nil, err
			}

			groups := make(map[string]*receiverFeature)
			for _, prod := range feats.Producers {
				if prod.IsGeneric {
					return nil, fmt.Errorf("ssr: package %s declares producer %s on generic type %s[…]; generic receivers cannot be instantiated as a zero value — use a concrete type", m.path, prod.Name+"()", prod.ReceiverType)
				}
				rf, ok := groups[prod.ReceiverType]
				if !ok {
					rf = &receiverFeature{Name: prod.ReceiverType}
					groups[prod.ReceiverType] = rf
				}
				switch prod.Name {
				case "RootCSS":
					rf.HasRoot = true
				case "RenderCSS":
					rf.HasRender = true
				case "RenderHTML":
					rf.HasHTML = true
				case "RenderJS":
					rf.HasJS = true
				case "IconSvg":
					rf.HasIcons = true
				case "Fonts":
					rf.HasFonts = true
				case "RenderPages":
					rf.HasPages = true
				}
			}

			var receivers []receiverFeature
			for _, rf := range groups {
				receivers = append(receivers, *rf)
			}
			sort.Slice(receivers, func(i, j int) bool {
				return receivers[i].Name < receivers[j].Name
			})

			if len(receivers) == 0 {
				absRoot, _ := filepath.Abs(rootDir)
				isLocal := absRoot != "" && strings.HasPrefix(m.dir, absRoot)
				if isLocal {
					if _, err := os.Stat(filepath.Join(m.dir, cssSourceFile)); err == nil {
						return nil, fmt.Errorf("ssr: package %s has %s but declares no RootCSS() or RenderCSS(); expected: func (w *T) RenderCSS() *css.Stylesheet", m.path, cssSourceFile)
					}

					var importedLib string
					for imp := range feats.Imports {
						for _, lib := range assetLibraries {
							if imp == lib || strings.HasSuffix(imp, "/"+lib) {
								importedLib = imp
								break
							}
						}
						if importedLib != "" {
							break
						}
					}
					if importedLib != "" {
						return nil, fmt.Errorf("ssr: package %s imports %s but declares no producer; expected: func (w *T) RenderCSS() *css.Stylesheet", m.path, importedLib)
					}
				}
			}

			ma.Receivers = receivers
		}

		aliases = append(aliases, ma)
	}

	if skippedCount > 0 && log != nil {
		log(fmt.Sprintf(skippedUnreachableFmt, skippedCount))
	}

	return aliases, nil
}
