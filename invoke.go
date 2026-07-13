package ssr

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"

	"github.com/tinywasm/fmt"
	"github.com/tinywasm/svg/sprite"
)

// ssrCollectorOutput is the structure produced by the generated main.go
type ssrCollectorOutput struct {
	Root    string      `json:"root"`
	Render  string      `json:"render"`
	HTML    string      `json:"html"`
	Scripts []ScriptOutput `json:"scripts"`
	Icons   *sprite.Sprite `json:"icons"`
}

type ScriptOutput struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type moduleAlias struct {
	Path         string
	Alias        string
	ReceiverType string
	HasRoot      bool
	HasRender    bool
	HasHTML      bool
	HasJS        bool
	HasIcons     bool
}

func (m moduleAlias) HasAnyFeature() bool {
	return m.HasRoot || m.HasRender || m.HasHTML || m.HasJS || m.HasIcons
}

// invokeSSRExtractorOnce generates a combined main.go, runs it once, and returns the aggregated output.
func invokeSSRExtractorOnce(rootDir string, modules []module) (map[string]ssrCollectorOutput, error) {
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
	var results map[string]ssrCollectorOutput
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
	{{range .Modules}}
	{{if .HasAnyFeature}}{{.Alias}} "{{.Path}}"{{end}}
	{{end}}
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

func main() {
	all := make(map[string]ssr)
	{{range .Modules}}
	{{if .HasAnyFeature}}
	{
		var s ssr
		{{if .ReceiverType}}
		{
			inst := &{{.Alias}}.{{.ReceiverType}}{}
			{{if .HasRoot}}s.Root = inst.RootCSS().String(){{end}}
			{{if .HasRender}}s.Render = inst.RenderCSS().String(){{end}}
			{{if .HasHTML}}s.HTML = inst.RenderHTML(){{end}}
			{{if .HasJS}}
			for _, scr := range inst.RenderJS() {
				s.Scripts = append(s.Scripts, script{Name: scr.Name, Content: scr.Content})
			}
			{{end}}
			{{if .HasIcons}}s.Icons = inst.IconSvg(){{end}}
		}
		{{else}}
		{
			{{if .HasRoot}}s.Root = {{.Alias}}.RootCSS().String(){{end}}
			{{if .HasRender}}s.Render = {{.Alias}}.RenderCSS().String(){{end}}
			{{if .HasHTML}}s.HTML = {{.Alias}}.RenderHTML(){{end}}
			{{if .HasJS}}
			for _, scr := range {{.Alias}}.RenderJS() {
				s.Scripts = append(s.Scripts, script{Name: scr.Name, Content: scr.Content})
			}
			{{end}}
			{{if .HasIcons}}s.Icons = {{.Alias}}.IconSvg(){{end}}
		}
		{{end}}
		all["{{.Path}}"] = s
	}
	{{end}}
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

// Feature detection matches on the METHOD NAME only — never on its return type or
// the package that type comes from. That is what let IconSvg() switch from
// *svg.Sprite to *sprite.Sprite without touching a single pattern here, and it is
// why the generated main.go can keep `Icons any`: it is compiled standalone against
// whatever the target module's own IconSvg() returns.
var (
	reRootCSS    = regexp.MustCompile(`(?m)^func \(\w+ \*?(\w+)\) RootCSS\(\)`)
	reRenderCSS  = regexp.MustCompile(`(?m)^func \(\w+ \*?(\w+)\) RenderCSS\(\)`)
	reRenderHTML = regexp.MustCompile(`(?m)^func \(\w+ \*?(\w+)\) RenderHTML\(\)`)
	reRenderJS   = regexp.MustCompile(`(?m)^func \(\w+ \*?(\w+)\) RenderJS\(\)`)
	reIconSvg    = regexp.MustCompile(`(?m)^func \(\w+ \*?(\w+)\) IconSvg\(\)`)

	// Fallback regexes for functions without receiver
	reRootCSSFunc    = regexp.MustCompile(`(?m)^func RootCSS\(\)`)
	reRenderCSSFunc  = regexp.MustCompile(`(?m)^func RenderCSS\(\)`)
	reRenderHTMLFunc = regexp.MustCompile(`(?m)^func RenderHTML\(\)`)
	reRenderJSFunc   = regexp.MustCompile(`(?m)^func RenderJS\(\)`)
	reIconSvgFunc    = regexp.MustCompile(`(?m)^func IconSvg\(\)`)
)

// expandToSSRPackages turns each module into the PACKAGES inside it that actually
// declare SSR sources.
//
// A real app keeps its RootCSS() in config/ and its views in modules/x/ — never at
// the module root. Reading the source files only at the module dir found nothing, so
// the generated main.go never called RootCSS(): the app shipped with an empty
// style.css and no icons, and nothing failed loudly.
//
// Only packages that carry an SSR source file are returned. That matters: the module
// root is usually `package main`, and a generated main.go cannot import it.
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

// modulesToAliases converts module information to alias mappings and detects features via regex.
func modulesToAliases(modules []module) []moduleAlias {
	var aliases []moduleAlias
	for _, m := range expandToSSRPackages(modules) {
		parts := strings.Split(m.path, "/")
		alias := strings.ReplaceAll(parts[len(parts)-1], "-", "_")

		// If alias starts with a digit or is empty, prepend an underscore to make it a valid Go identifier
		if len(alias) == 0 || (alias[0] >= '0' && alias[0] <= '9') {
			alias = "_" + alias
		}


		ma := moduleAlias{
			Path:  m.path,
			Alias: alias,
		}

		// Read all SSR source files to detect features
		if m.dir != "" {
			var combinedContent []byte
			for _, f := range ssrSourceFiles {
				if content, err := os.ReadFile(filepath.Join(m.dir, f)); err == nil {
					combinedContent = append(combinedContent, content...)
					combinedContent = append(combinedContent, '\n')
				}
			}

			if len(combinedContent) > 0 {
				// Detect receiver type
				ma.ReceiverType = detectReceiverType(combinedContent)

				if ma.ReceiverType != "" {
					ma.HasRoot = reRootCSS.Match(combinedContent)
					ma.HasRender = reRenderCSS.Match(combinedContent)
					ma.HasHTML = reRenderHTML.Match(combinedContent)
					ma.HasJS = reRenderJS.Match(combinedContent)
					ma.HasIcons = reIconSvg.Match(combinedContent)
				} else {
					ma.HasRoot = reRootCSSFunc.Match(combinedContent)
					ma.HasRender = reRenderCSSFunc.Match(combinedContent)
					ma.HasHTML = reRenderHTMLFunc.Match(combinedContent)
					ma.HasJS = reRenderJSFunc.Match(combinedContent)
					ma.HasIcons = reIconSvgFunc.Match(combinedContent)
				}
			}
		}

		aliases = append(aliases, ma)
	}
	return aliases
}

func detectReceiverType(content []byte) string {
	regs := []*regexp.Regexp{reRootCSS, reRenderCSS, reRenderHTML, reRenderJS, reIconSvg}
	var detected string
	for _, re := range regs {
		m := re.FindSubmatch(content)
		if len(m) > 1 {
			found := string(m[1])
			if detected != "" && detected != found {
				continue
			}
			detected = found
		}
	}
	return detected
}
