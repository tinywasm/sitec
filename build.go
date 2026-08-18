package sitec

import (
	"os"
	"path/filepath"

	"github.com/tinywasm/fmt"
	"github.com/tinywasm/image/min"
)

const (
	DefaultOutputDir    = "web/public"
	DefaultDevOutputDir = ".tinywasm/public"
	DefaultImageQuality = 82
)

// Mode decides what artifact Build produces.
type Mode uint8

const (
	// ModeRelease is the deliverable: WASM via TinyGo, minified.
	ModeRelease Mode = iota
	// ModeDev is the development cache: fast compilation, unminified.
	ModeDev
)

// BuildConfig contains the site build settings.
type BuildConfig struct {
	RootDir        string   // Root directory of the module (where go.mod lives). Required.
	Mode           Mode
	OutputDir      string   // Relative to RootDir. Empty => DefaultOutputDir (Release) or DefaultDevOutputDir (Dev).
	SiteURL        string   // Enables sitemap.xml and absolute canonical URLs.
	AppName        string
	StaticAssets   []string // Declared static assets relative to RootDir copied verbatim.
	ImageQuality   int      // 0 => DefaultImageQuality
	AssetLibraries []string // Style libraries whose importers must declare a producer.
	Log            func(...any)
}

// Site is the result of a COMPLETE build.
type Site struct {
	am *AssetMin
}

// Artifacts returns all produced artifacts.
func (s *Site) Artifacts() []Artifact {
	if s == nil || s.am == nil {
		return nil
	}
	return s.am.List()
}

// WriteTo writes the built site artifacts to the given FS.
func (s *Site) WriteTo(fs FS) error {
	if s == nil || s.am == nil {
		return fmt.Err("sitec: WriteTo called on nil Site")
	}
	for _, art := range s.Artifacts() {
		if err := fs.Write(art.Path, art.Content, art.Mediatype); err != nil {
			return err
		}
	}
	return nil
}

// Build executes the entire build pipeline in memory. It does not write to the output disk.
func Build(cfg BuildConfig) (*Site, error) {
	if cfg.RootDir == "" {
		return nil, fmt.Err("sitec: RootDir is required")
	}

	root, err := filepath.Abs(cfg.RootDir)
	if err != nil {
		return nil, fmt.Err("sitec: error resolving RootDir:", err)
	}

	if err := ValidateProject(root); err != nil {
		return nil, err
	}

	e := New(root)
	if cfg.Log != nil {
		e.SetLog(cfg.Log)
	}
	// Solo se sobrescribe si el llamador declaró su propia lista: pasar nil no
	// debe apagar la comprobación que New() deja encendida.
	if len(cfg.AssetLibraries) > 0 {
		e.SetAssetLibraries(cfg.AssetLibraries)
	}

	if _, err := os.Stat(filepath.Join(root, "web", "client.go")); err == nil {
		e.SetWasmBuilder(NewDefaultWasmBuilder(cfg.Mode == ModeDev))
	}

	all, err := e.ExtractAll()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, fmt.Err("sitec: extracción vacía: ningún módulo aportó assets")
	}

	outDir := cfg.OutputDir
	if outDir == "" {
		if cfg.Mode == ModeDev {
			outDir = DefaultDevOutputDir
		} else {
			outDir = DefaultOutputDir
		}
	}

	imgQuality := cfg.ImageQuality
	if imgQuality == 0 {
		imgQuality = DefaultImageQuality
	}

	am := NewAssetMin(&Config{
		OutputDir: outDir,
		RootDir:   root,
		AppName:   cfg.AppName,
		SiteURL:   cfg.SiteURL,
		DevMode:   cfg.Mode == ModeDev,
	})
	if cfg.Log != nil {
		am.SetLog(cfg.Log)
	}
	am.SetFS(NewMemFS())

	imgHandler := min.New(&min.Config{
		RootDir:   root,
		OutputDir: filepath.Join(outDir, "img"),
		Quality:   imgQuality,
	})
	if cfg.Log != nil {
		imgHandler.SetLog(cfg.Log)
	}
	imgHandler.SetFinder(e.Finder())
	am.SetImageProcessor(imgHandler)

	wb := e.WasmBuilder()
	if wb != nil {
		wasmOut, err := wb.Build(root)
		if err != nil {
			return nil, err
		}
		am.SetWasm(wasmOut.Filename, wasmOut.Runtime)
		if err := am.Write(wasmOut.Filename, wasmOut.Binary, "application/wasm"); err != nil {
			return nil, err
		}
	}

	if err := am.RouteExtractedAssets(all); err != nil {
		return nil, err
	}

	if err := imgHandler.LoadImages(); err != nil {
		return nil, err
	}

	if err := copyStaticAssets(am, root, cfg.StaticAssets); err != nil {
		return nil, err
	}

	return &Site{am: am}, nil
}

// Check validates the project extraction and returns the list of extracted module names.
func Check(rootDir string, log func(...any)) ([]string, error) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Err("sitec: error resolving RootDir:", err)
	}

	if err := ValidateProject(root); err != nil {
		return nil, err
	}

	e := New(root)
	if log != nil {
		e.SetLog(log)
	}

	all, err := e.ExtractAll()
	if err != nil {
		return nil, err
	}

	var modules []string
	for _, a := range all {
		if a != nil {
			modules = append(modules, a.ModuleName)
		}
	}
	return modules, nil
}

func copyStaticAssets(am *AssetMin, rootDir string, staticAssets []string) error {
	for _, entry := range staticAssets {
		srcPath := filepath.Join(rootDir, entry)
		info, err := os.Stat(srcPath)
		if err != nil {
			return fmt.Err("sitec: activo estático declarado y ausente:", entry)
		}

		if info.IsDir() {
			err = filepath.Walk(srcPath, func(p string, fi os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if fi.IsDir() {
					return nil
				}
				rel, err := filepath.Rel(rootDir, p)
				if err != nil {
					return err
				}
				content, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				mt := detectMediaType(p)
				return am.Write(rel, content, mt)
			})
			if err != nil {
				return err
			}
		} else {
			content, err := os.ReadFile(srcPath)
			if err != nil {
				return err
			}
			mt := detectMediaType(srcPath)
			if err := am.Write(entry, content, mt); err != nil {
				return err
			}
		}
	}
	return nil
}

func detectMediaType(p string) string {
	switch filepath.Ext(p) {
	case ".css":
		return "text/css"
	case ".js":
		return "text/javascript"
	case ".svg":
		return "image/svg+xml"
	case ".html":
		return "text/html"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".json":
		return "application/json"
	case ".wasm":
		return "application/wasm"
	default:
		return "text/plain"
	}
}
