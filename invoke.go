package ssr

import (
	"bytes"
	"encoding/json"
	stdFmt "fmt"
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
	"reflect"
	"github.com/tinywasm/widget"
	{{range .Modules}}
	{{.Alias}} "{{.Path}}"
	{{end}}
)

type script struct {
	Name    string ` + "`json:\"name\"`" + `
	Content string ` + "`json:\"content\"`" + `
}

type ssr struct {
	Render  string   ` + "`json:\"render\"`" + `
	HTML    string   ` + "`json:\"html\"`" + `
	Scripts []script ` + "`json:\"scripts\"`" + `
	Icons   any      ` + "`json:\"icons\"`" + `
}

func collect(parts ...widget.Widget) ssr {
	var s ssr
	for _, p := range parts {
		if p == nil {
			continue
		}
		val := reflect.ValueOf(p)

		// Style()
		if method := val.MethodByName("Style"); method.IsValid() {
			sheetResults := method.Call(nil)
			if len(sheetResults) > 0 && !sheetResults[0].IsNil() {
				sheet := sheetResults[0]
				if stylesheetMethod := sheet.MethodByName("Stylesheet"); stylesheetMethod.IsValid() {
					stylesheetResults := stylesheetMethod.Call(nil)
					if len(stylesheetResults) > 0 && !stylesheetResults[0].IsNil() {
						stylesheet := stylesheetResults[0]
						if stringMethod := stylesheet.MethodByName("String"); stringMethod.IsValid() {
							s.Render += stringMethod.Call(nil)[0].String()
						}
					}
				}
			}
		}

		// Icons()
		if method := val.MethodByName("Icons"); method.IsValid() {
			iconsResults := method.Call(nil)
			if len(iconsResults) > 0 && !iconsResults[0].IsNil() {
				s.Icons = iconsResults[0].Interface()
			}
		}

		// HTML()
		if method := val.MethodByName("HTML"); method.IsValid() {
			htmlResults := method.Call(nil)
			if len(htmlResults) > 0 {
				s.HTML += htmlResults[0].String()
			}
		}

		// JS()
		if method := val.MethodByName("JS"); method.IsValid() {
			jsResults := method.Call(nil)
			if len(jsResults) > 0 && !jsResults[0].IsNil() {
				scriptsSlice := jsResults[0]
				for i := 0; i < scriptsSlice.Len(); i++ {
					scriptVal := scriptsSlice.Index(i)
					if !scriptVal.IsNil() {
						nameVal := scriptVal.Elem().FieldByName("Name")
						contentVal := scriptVal.Elem().FieldByName("Content")
						s.Scripts = append(s.Scripts, script{
							Name:    nameVal.String(),
							Content: contentVal.String(),
						})
					}
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
// declare SSR sources and implement the SSR() function.
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
			if !hasSSRSource(path) || !packageHasSSRFunction(path) {
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

func packageHasSSRFunction(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}
			if strings.Contains(string(content), "func SSR(") || strings.Contains(string(content), "func SSR (") {
				return true
			}
		}
	}
	return false
}

// modulesToAliases converts module information to alias mappings using safe indexed prefix m%d.
func modulesToAliases(modules []module) []moduleAlias {
	var aliases []moduleAlias
	for i, m := range expandToSSRPackages(modules) {
		alias := stdFmt.Sprintf("m%d", i)
		aliases = append(aliases, moduleAlias{
			Path:  m.path,
			Alias: alias,
		})
	}
	return aliases
}
