package ssr

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/tinywasm/fmt"
	"github.com/tinywasm/font"
	"github.com/tinywasm/svg/sprite"
)

// CollectorOutput is the structure produced by the generated main.go
type CollectorOutput struct {
	Root    string           `json:"root"`
	Render  string           `json:"render"`
	HTML    string           `json:"html"`
	Scripts []ScriptOutput   `json:"scripts"`
	Icons   *sprite.Sprite   `json:"icons"`
	Fonts   font.Declaration `json:"fonts"`
}

// fontsWire is the JSON shape for a Declaration (unexported fields cannot marshal).
type fontsWire struct {
	Family string `json:"family"`
	Dir    string `json:"dir"`
}

type ScriptOutput struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

const (
	aliasPrefix   = "m_"
	cssSourceFile = "css.go"
)

type receiverFeature struct {
	Name      string
	HasRoot   bool
	HasRender bool
	HasHTML   bool
	HasJS     bool
	HasIcons  bool
	HasFonts  bool
}

type moduleAlias struct {
	Path      string
	Alias     string
	Receivers []receiverFeature
}

func (m moduleAlias) HasAnyFeature() bool {
	return len(m.Receivers) > 0
}

// invokeSSRExtractorOnce generates a combined main.go, runs it once, and returns the aggregated output.
func invokeSSRExtractorOnce(rootDir string, modules []module, scanner *scanner, assetLibraries []string) (map[string]CollectorOutput, error) {
	// Create a temporary hidden directory within rootDir to ensure we are in the module context.
	tmpDir := filepath.Join(rootDir, ".ssr_extract")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Err("failed to create temp dir", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate main.go that imports all modules
	mainFile := filepath.Join(tmpDir, "main.go")
	if err := GenerateExtractorMain(mainFile, modules, scanner, assetLibraries); err != nil {
		return nil, fmt.Err("failed to generate main.go", err)
	}

	// Run go run main.go and capture JSON output
	cmd := exec.Command("go", "run", "main.go")
	cmd.Dir = tmpDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, fmt.Err(errMsg)
	}

	type rawCollectorOutput struct {
		Root    string            `json:"root"`
		Render  string            `json:"render"`
		HTML    string            `json:"html"`
		Scripts []ScriptOutput    `json:"scripts"`
		Icons   []json.RawMessage `json:"icons"`
		Fonts   fontsWire         `json:"fonts"`
	}

	// Parse the JSON output
	var results map[string]rawCollectorOutput
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, fmt.Err("failed to parse extractor output", err)
	}

	finalResults := make(map[string]CollectorOutput)
	for pkg, raw := range results {
		var mergedSprite *sprite.Sprite
		for _, rawIcon := range raw.Icons {
			if len(rawIcon) == 0 || string(rawIcon) == "null" {
				continue
			}
			var sp *sprite.Sprite
			if err := json.Unmarshal(rawIcon, &sp); err != nil {
				return nil, fmt.Err("failed to unmarshal icon sprite for", pkg, err)
			}
			if sp != nil {
				if mergedSprite == nil {
					mergedSprite = sprite.NewSprite()
				}
				mergedSprite.Merge(sp)
			}
		}
		var fonts font.Declaration
		if raw.Fonts.Family != "" {
			fonts = font.Declare(font.Family(raw.Fonts.Family), raw.Fonts.Dir)
		}
		finalResults[pkg] = CollectorOutput{
			Root:    raw.Root,
			Render:  raw.Render,
			HTML:    raw.HTML,
			Scripts: raw.Scripts,
			Icons:   mergedSprite,
			Fonts:   fonts,
		}
	}

	return finalResults, nil
}

// GenerateExtractorMain writes a main.go file that imports all modules and collects their assets.
func GenerateExtractorMain(outputFile string, modules []module, scanner *scanner, assetLibraries []string) error {
	tmpl := template.Must(template.New("extractor").Parse(`package main

import (
	"encoding/json"
	"fmt"
	"os"
	{{range .Modules}}
	{{if .HasAnyFeature}}{{.Alias}} "{{.Path}}"{{end}}
	{{end}}
)

type script struct {
	Name    string ` + "`json:\"name\"`" + `
	Content string ` + "`json:\"content\"`" + `
}

type fontsWire struct {
	Family string ` + "`json:\"family\"`" + `
	Dir    string ` + "`json:\"dir\"`" + `
}

type ssr struct {
	Root    string    ` + "`json:\"root\"`" + `
	Render  string    ` + "`json:\"render\"`" + `
	HTML    string    ` + "`json:\"html\"`" + `
	Scripts []script  ` + "`json:\"scripts\"`" + `
	Icons   []any     ` + "`json:\"icons\"`" + `
	Fonts   fontsWire ` + "`json:\"fonts\"`" + `
}

type failure struct {
	Pkg  string ` + "`json:\"pkg\"`" + `
	Type string ` + "`json:\"type\"`" + `
	Err  string ` + "`json:\"err\"`" + `
}

func main() {
	all := make(map[string]ssr)
	var failures []failure

	{{range .Modules}}
	{{if .HasAnyFeature}}
	{
		var s ssr
		{{$alias := .Alias}}
		{{$path := .Path}}
		{{range .Receivers}}
		func() {
			defer func() {
				if r := recover(); r != nil {
					failures = append(failures, failure{Pkg: "{{$path}}", Type: "{{.Name}}", Err: fmt.Sprint(r)})
				}
			}()
			{{if .Name}}
			inst := &{{$alias}}.{{.Name}}{}
			{{if .HasRoot}}s.Root += inst.RootCSS().String(){{end}}
			{{if .HasRender}}s.Render += inst.RenderCSS().String(){{end}}
			{{if .HasHTML}}s.HTML += inst.RenderHTML(){{end}}
			{{if .HasJS}}
			for _, scr := range inst.RenderJS() {
				s.Scripts = append(s.Scripts, script{Name: scr.Name, Content: scr.Content})
			}
			{{end}}
			{{if .HasIcons}}s.Icons = append(s.Icons, inst.IconSvg()){{end}}
			{{if .HasFonts}}
			{
				d := inst.Fonts()
				s.Fonts = fontsWire{Family: string(d.Family()), Dir: d.Dir()}
			}
			{{end}}
			{{else}}
			{{if .HasRoot}}s.Root += {{$alias}}.RootCSS().String(){{end}}
			{{if .HasRender}}s.Render += {{$alias}}.RenderCSS().String(){{end}}
			{{if .HasHTML}}s.HTML += {{$alias}}.RenderHTML(){{end}}
			{{if .HasJS}}
			for _, scr := range {{$alias}}.RenderJS() {
				s.Scripts = append(s.Scripts, script{Name: scr.Name, Content: scr.Content})
			}
			{{end}}
			{{if .HasIcons}}s.Icons = append(s.Icons, {{$alias}}.IconSvg()){{end}}
			{{if .HasFonts}}
			{
				d := {{$alias}}.Fonts()
				s.Fonts = fontsWire{Family: string(d.Family()), Dir: d.Dir()}
			}
			{{end}}
			{{end}}
		}()
		{{end}}
		all["{{$path}}"] = s
	}
	{{end}}
	{{end}}

	if len(failures) > 0 {
		for _, f := range failures {
			fmt.Fprintf(os.Stderr, "ssr: producer panic in package %s, type %s: %s\n", f.Pkg, f.Type, f.Err)
		}
		os.Exit(1)
	}

	json.NewEncoder(os.Stdout).Encode(all)
}
`))

	aliases, err := modulesToAliases(modules, scanner, assetLibraries)
	if err != nil {
		return err
	}

	data := struct {
		Modules []moduleAlias
	}{
		Modules: aliases,
	}

	f, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, data)
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

// modulesToAliases converts module information to alias mappings and detects features.
func modulesToAliases(modules []module, scanner *scanner, assetLibraries []string) ([]moduleAlias, error) {
	var aliases []moduleAlias
	for _, m := range expandToSSRPackages(modules, scanner, assetLibraries) {
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
					return nil, fmt.Err("ssr: package", m.path,
						"declares producer", prod.Name+"()",
						"on generic type", prod.ReceiverType+"[…];",
						"generic receivers cannot be instantiated as a zero value — use a concrete type")
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
				if _, err := os.Stat(filepath.Join(m.dir, cssSourceFile)); err == nil {
					return nil, fmt.Err("ssr: package", m.path,
						"has "+cssSourceFile+" but declares no RootCSS() or RenderCSS();",
						"expected: func (w *T) RenderCSS() *css.Stylesheet")
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
					return nil, fmt.Err("ssr: package", m.path, "imports", importedLib,
						"but declares no producer; expected: func (w *T) RenderCSS() *css.Stylesheet")
				}
			}

			ma.Receivers = receivers
		}

		aliases = append(aliases, ma)
	}
	return aliases, nil
}
