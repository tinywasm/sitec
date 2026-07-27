package ssr

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/tinywasm/fmt"
)

type moduleAlias struct {
	Path  string
	Alias string
}

// invokeSSRExtractorOnce generates a combined main.go, runs it once, and returns the aggregated output.
func invokeSSRExtractorOnce(rootDir string, modules []module) (map[string]Bundle, error) {
	// Create a temporary hidden directory within rootDir to ensure we are in the module context.
	tmpDir := filepath.Join(rootDir, ".ssr_extract")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Err("failed to create temp dir", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate main.go that imports all modules
	mainFile := filepath.Join(tmpDir, "main.go")
	if err := GenerateExtractorMain(mainFile, modules); err != nil {
		return nil, fmt.Err("failed to generate main.go", err)
	}

	// Run go run main.go and capture JSON output
	cmd := exec.Command("go", "run", "main.go")
	cmd.Dir = tmpDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Err("go run failed", err, stderr.String())
	}

	// Parse the JSON output
	var results map[string]Bundle
	if err := json.Unmarshal(out, &results); err != nil {
		return nil, fmt.Err("failed to parse extractor output", err)
	}

	return results, nil
}

// GenerateExtractorMain writes a main.go file that imports all modules and collects their assets.
func GenerateExtractorMain(outputFile string, modules []module) error {
	tmpl := template.Must(template.New("extractor").Parse(`package main

import (
	"encoding/json"
	"os"
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
	"github.com/tinywasm/js"
	"github.com/tinywasm/svg/sprite"
	{{range .Modules}}
	{{.Alias}} "{{.Path}}"
	{{end}}
)

var (
	_ = widget.Region
	_ = style.RadiusNone
	_ = js.RuntimeGo
	_ = sprite.Path
)

type script struct {
	Name    string ` + "`json:\"name\"`" + `
	Content string ` + "`json:\"content\"`" + `
}

type ssr struct {
	Root    string   ` + "`json:\"root\"`" + `
	Render  string   ` + "`json:\"render\"`" + `
	HTML    string   ` + "`json:\"html\"`" + `
	Scripts []script ` + "`json:\"scripts\"`" + `
	Icons   any      ` + "`json:\"icons\"`" + `
}

type Styler interface {
	Style() *style.Sheet
}

type HTMLProvider interface {
	HTML() string
}

type JSProvider interface {
	JS() []*js.Script
}

type IconProvider interface {
	Icons() *sprite.Sprite
}

func collect(parts ...widget.Widget) ssr {
	var s ssr
	s.Icons = sprite.NewSprite()
	for _, p := range parts {
		if styler, ok := p.(Styler); ok {
			sheet := styler.Style()
			if sheet != nil {
				s.Render += sheet.Stylesheet().String()
			}
		}
		if iconProv, ok := p.(IconProvider); ok {
			icons := iconProv.Icons()
			if icons != nil {
				if spr, ok := s.Icons.(*sprite.Sprite); ok {
					spr.Merge(icons)
				}
			}
		}
		if htmlProv, ok := p.(HTMLProvider); ok {
			s.HTML += htmlProv.HTML()
		}
		if jsProv, ok := p.(JSProvider); ok {
			for _, scr := range jsProv.JS() {
				if scr != nil {
					s.Scripts = append(s.Scripts, script{Name: scr.Name, Content: scr.Content})
				}
			}
		}
	}
	return s
}

func main() {
	all := make(map[string]ssr)
	{{range .Modules}}
	{
		all["{{.Path}}"] = collect({{.Alias}}.SSR()...)
	}
	{{end}}
	json.NewEncoder(os.Stdout).Encode(all)
}
`))

	data := struct {
		Modules []moduleAlias
	}{
		Modules: modulesToAliases(modules),
	}

	f, err := os.Create(outputFile)
	if err != nil {
		return err
	}
	defer f.Close()

	return tmpl.Execute(f, data)
}

// expandToSSRPackages turns each module into the PACKAGES inside it that actually
// declare SSR sources.
func expandToSSRPackages(modules []module) []module {
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
			if !hasSSRSource(path) {
				return nil
			}

			pkgPath := m.path
			if rel, err := filepath.Rel(m.dir, path); err == nil && rel != "." {
				pkgPath = m.path + "/" + filepath.ToSlash(rel)
			}
			if !seen[pkgPath] {
				seen[pkgPath] = true
				out = append(out, module{path: pkgPath, dir: path})
			}
			return nil
		})
	}
	return out
}

func hasSSRSource(dir string) bool {
	for _, f := range ssrSourceFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

// modulesToAliases converts module information to alias mappings.
func modulesToAliases(modules []module) []moduleAlias {
	var aliases []moduleAlias
	for _, m := range expandToSSRPackages(modules) {
		parts := strings.Split(m.path, "/")
		alias := strings.ReplaceAll(parts[len(parts)-1], "-", "_")

		// If alias starts with a digit or is empty, prepend an underscore to make it a valid Go identifier
		if len(alias) == 0 || (alias[0] >= '0' && alias[0] <= '9') {
			alias = "_" + alias
		}

		aliases = append(aliases, moduleAlias{
			Path:  m.path,
			Alias: alias,
		})
	}
	return aliases
}
