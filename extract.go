package sitec

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	aliasPrefix     = "m_"
	cssSourceFile   = "css.go"
	mainPackageName = "main"
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
func invokeSSRExtractorOnce(projectRoot string, startDir string, modules []module, scanner *scanner, assetLibraries []string, lister GraphLister, log func(...any), toolchain Toolchain) (map[string]CollectorOutput, error) {
	// Create a temporary hidden directory within projectRoot to ensure we are in the module context.
	tmpDir := filepath.Join(projectRoot, ".ssr_extract")
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Err("failed to create temp dir", err)
	}
	defer os.RemoveAll(tmpDir)

	// Generate main.go that imports all modules
	mainFile := filepath.Join(tmpDir, "main.go")
	if err := GenerateExtractorMain(mainFile, modules, scanner, assetLibraries, startDir, lister, log); err != nil {
		return nil, fmt.Err("failed to generate main.go", err)
	}

	// Run go run main.go and capture JSON output using the toolchain port
	out, err := toolchain.Run(tmpDir, "run", "main.go")
	if err != nil {
		return nil, err
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
func GenerateExtractorMain(outputFile string, modules []module, scanner *scanner, assetLibraries []string, startDir string, lister GraphLister, log func(...any)) error {
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

	aliases, err := modulesToAliases(modules, scanner, assetLibraries, startDir, lister, log)
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

