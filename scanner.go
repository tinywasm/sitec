package sitec

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/tinywasm/fmt"
)

type producerDecl struct {
	Name         string
	ReceiverType string
	IsGeneric    bool
}

type fileFeatures struct {
	mtime     time.Time
	pkgName   string          // f.Name.Name del archivo parseado
	imports   map[string]bool
	producers []producerDecl
}

type scanner struct {
	mu    sync.RWMutex
	cache map[string]fileFeatures
}

func newScanner() *scanner {
	return &scanner{
		cache: make(map[string]fileFeatures),
	}
}

func getReceiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return getReceiverTypeName(t.X)
	case *ast.IndexExpr:
		return getReceiverTypeName(t.X)
	case *ast.IndexListExpr:
		return getReceiverTypeName(t.X)
	}
	return ""
}

func isGenericReceiver(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return isGenericReceiver(t.X)
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	}
	return false
}

func (s *scanner) scanFile(path string) (fileFeatures, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileFeatures{}, fmt.Err("failed to stat file:", err)
	}

	s.mu.RLock()
	cached, ok := s.cache[path]
	s.mu.RUnlock()

	if ok && cached.mtime.Equal(info.ModTime()) {
		return cached, nil
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return fileFeatures{}, fmt.Err("failed to parse file:", err)
	}

	imports := make(map[string]bool)
	for _, imp := range f.Imports {
		if imp.Path != nil {
			p := strings.Trim(imp.Path.Value, "\"")
			imports[p] = true
		}
	}

	var producers []producerDecl
	producerNames := map[string]bool{
		"RootCSS":    true,
		"RenderCSS":  true,
		"RenderHTML": true,
		"RenderJS":   true,
		"IconSvg":    true,
		"Fonts":      true,
	}

	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if !producerNames[fn.Name.Name] {
			continue
		}

		var recvType string
		var isGen bool
		if fn.Recv != nil && len(fn.Recv.List) > 0 {
			recvType = getReceiverTypeName(fn.Recv.List[0].Type)
			isGen = isGenericReceiver(fn.Recv.List[0].Type)
		}

		producers = append(producers, producerDecl{
			Name:         fn.Name.Name,
			ReceiverType: recvType,
			IsGeneric:    isGen,
		})
	}

	features := fileFeatures{
		mtime:     info.ModTime(),
		pkgName:   f.Name.Name,
		imports:   imports,
		producers: producers,
	}

	s.mu.Lock()
	s.cache[path] = features
	s.mu.Unlock()

	return features, nil
}

type packageFeatures struct {
	PkgName   string
	Imports   map[string]bool
	Producers []producerDecl
}

func (s *scanner) scanPackage(dir string) (packageFeatures, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return packageFeatures{}, nil
		}
		return packageFeatures{}, fmt.Err("failed to read dir:", err)
	}

	agg := packageFeatures{
		Imports: make(map[string]bool),
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			path := filepath.Join(dir, entry.Name())
			feats, err := s.scanFile(path)
			if err != nil {
				continue // Skip/ignore file error
			}
			if agg.PkgName == "" {
				agg.PkgName = feats.pkgName
			}
			for imp := range feats.imports {
				agg.Imports[imp] = true
			}
			agg.Producers = append(agg.Producers, feats.producers...)
		}
	}

	return agg, nil
}

func (s *scanner) ScanProjectImports(rootDir string) (map[string]bool, error) {
	allImports := make(map[string]bool)

	// Files in rootDir
	err := s.scanDir(rootDir, allImports)
	if err != nil {
		return nil, err
	}

	// Files in one level of subdirectories
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Skip hidden dirs (like .git)
			if strings.HasPrefix(entry.Name(), ".") {
				continue
			}
			err := s.scanDir(filepath.Join(rootDir, entry.Name()), allImports)
			if err != nil {
				return nil, err
			}
		}
	}

	return allImports, nil
}

func (s *scanner) scanDir(dir string, allImports map[string]bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			path := filepath.Join(dir, entry.Name())
			feats, err := s.scanFile(path)
			if err != nil {
				continue // Skip problematic files
			}
			for imp := range feats.imports {
				allImports[imp] = true
			}
		}
	}
	return nil
}

func moduleSubpackagesUsed(modulePath string, moduleDir string, importedPaths map[string]bool) []string {
	var usedSubpackages []string
	seen := make(map[string]bool)

	for imp := range importedPaths {
		if imp == modulePath {
			if !seen[""] {
				usedSubpackages = append(usedSubpackages, "")
				seen[""] = true
			}
			continue
		}

		if strings.HasPrefix(imp, modulePath+"/") {
			subPath := strings.TrimPrefix(imp, modulePath+"/")

			// Support any depth — allow multi-segment paths like "modules/contact"
			if !seen[subPath] {
				usedSubpackages = append(usedSubpackages, subPath)
				seen[subPath] = true
			}
		}
	}

	return usedSubpackages
}
