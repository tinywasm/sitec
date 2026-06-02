# PLAN: tinywasm/ssr — Extractor de assets SSR

## Repositorio
`github.com/tinywasm/ssr` — path local: `tinywasm/ssr/`

Estado actual:
- `ssr.go` — stub `type Ssr struct{}` + `func New() *Ssr` (reemplazar)
- `invoke_original.go`, `extract_original.go`, `cache_original.go`, `import_scanner_original.go`
  — código fuente original recuperado de assetmin v0.3.5 (package assetmin, adaptar)

## Propósito

Absorber el extractor SSR de assetmin. assetmin define el contrato; ssr implementa.
Patrón simétrico a `image/min`.

## Contrato (definido en assetmin v0.4.0)

```go
// github.com/tinywasm/assetmin
type SSRAssets struct {
    ModuleName  string
    RootCSS     string
    CSS         string
    JS          []*js.Script
    HTML        string
    Icons       *svg.Sprite
    IsRoot      bool
    IsFramework bool
}

type SSRExtractor interface {
    ExtractModule(moduleDir string) (*SSRAssets, error)
    ExtractAll() ([]*SSRAssets, error)
}
```

---

## Paso 1: Reemplazar el stub en `ssr.go`

Eliminar el contenido actual de `ssr.go` y escribir:

```go
package ssr

import (
    "github.com/tinywasm/assetmin"
    "github.com/tinywasm/fmt"
)

const cssModulePath = "tinywasm/css"

type Extractor struct {
    rootDir       string
    listModulesFn func(rootDir string) ([]string, error)
    log           func(...any)
    cache         *ssrCache
}

func New(rootDir string) *Extractor {
    return &Extractor{
        rootDir: rootDir,
        log:     func(...any) {},
        cache:   newSSRCache(),
    }
}

func (e *Extractor) SetLog(fn func(...any))                              { e.log = fn }
func (e *Extractor) SetListModulesFn(fn func(string) ([]string, error)) { e.listModulesFn = fn }

func (e *Extractor) ExtractModule(moduleDir string) (*assetmin.SSRAssets, error) {
    rootDir, err := findProjectRoot(moduleDir)
    if err != nil {
        return nil, fmt.Err("find project root:", err)
    }
    modules, err := e.discoverModules(rootDir)
    if err != nil {
        modules = []module{{path: moduleDir, dir: moduleDir}}
    }
    var target module
    for _, m := range modules {
        if m.dir == moduleDir {
            target = m
            break
        }
    }
    if target.dir == "" {
        target = module{path: moduleDir, dir: moduleDir}
    }
    a, err := extractAssetsForModule(target, rootDir, modules, "", e.cache, e.log)
    if err != nil || a == nil {
        return nil, err
    }
    a.IsRoot = isRootDir(moduleDir, e.rootDir)
    a.IsFramework = isFrameworkModule(target.path)
    return a, nil
}

func (e *Extractor) ExtractAll() ([]*assetmin.SSRAssets, error) {
    modules, err := e.discoverModules(e.rootDir)
    if err != nil {
        return nil, err
    }
    var all []*assetmin.SSRAssets
    for _, m := range modules {
        a, err := extractAssetsForModule(m, e.rootDir, modules, "", e.cache, e.log)
        if err != nil {
            e.log("ssr extract error:", m.path, err)
            continue
        }
        if a != nil {
            a.IsRoot = isRootDir(m.dir, e.rootDir)
            a.IsFramework = isFrameworkModule(m.path)
            all = append(all, a)
        }
    }
    return all, nil
}

func (e *Extractor) discoverModules(rootDir string) ([]module, error) {
    if e.listModulesFn != nil {
        dirs, err := e.listModulesFn(rootDir)
        if err != nil {
            return nil, err
        }
        var mods []module
        for _, d := range dirs {
            mods = append(mods, module{path: d, dir: d})
        }
        return mods, nil
    }
    return discoverModules(rootDir)
}

func isRootDir(dir, rootDir string) bool {
    if rootDir == "" { return false }
    return dir == rootDir
}

func isFrameworkModule(path string) bool {
    return path == cssModulePath || hasSuffix(path, "/"+cssModulePath)
}
```

---

## Paso 2: Adaptar los archivos `*_original.go`

Para cada archivo renombrar y adaptar **sin reescribir lógica**:

### `invoke_original.go` → `invoke.go`

Cambios:
1. `package assetmin` → `package ssr`
2. Tipo `Module` (usado internamente) → `module` (minúscula, privado al paquete)
3. Tipo `SSRAssets` como retorno → `*assetmin.SSRAssets` (importado)
4. `ssrCollectorOutput.Icons` era `map[string]string` → cambiar a `*svg.Sprite`
   - En la struct del JSON del proceso generado: `Icons *svg.Sprite \`json:"Icons"\``
   - `svg.Sprite` ya tiene `MarshalJSON`/`UnmarshalJSON` en svg v0.0.3
5. Al construir el `assetmin.SSRAssets` de retorno, asignar `Icons: output.Icons`
6. El template del `main.go` generado NO cambia salvo los imports de los módulos compilados

### `extract_original.go` → `extract.go`

Cambios:
1. `package assetmin` → `package ssr`
2. `Module` → `module` (minúscula)
3. La función `extractSSRAssetsForModule` → `extractAssetsForModule` (firma ajustada a `module`)
4. El retorno es `*assetmin.SSRAssets` (en lugar de `*SSRAssets` local)
5. `IsRoot`/`IsFramework` NO se calculan aquí — se calculan en `ssr.go` (ExtractModule/ExtractAll)
6. Mantener toda la lógica de `ssrSourceFiles` detection y llamada al invoke

### `cache_original.go` → `cache.go`

Cambios:
1. `package assetmin` → `package ssr`
2. `SSRAssets` → `assetmin.SSRAssets` como tipo cacheado
3. Todo lo demás igual

### `import_scanner_original.go` → `scanner.go`

Cambios:
1. `package assetmin` → `package ssr`
2. Renombrar `importScanner` → `scanner` (o mantener nombre, privado igual)
3. Usado internamente por `extract.go` para `moduleSubpackagesUsed`
4. Todo lo demás igual — NO intentar reemplazar con depfind en este PR

---

## Paso 3: go.mod

```
module github.com/tinywasm/ssr

go 1.25

require (
    github.com/tinywasm/assetmin v0.4.0
    github.com/tinywasm/svg v0.0.3
    github.com/tinywasm/fmt v0.23.10
)
```

Ejecutar `go mod tidy`.

---

## Paso 4: Tests

El archivo `tests/extract_subpackage_original_test.go` ya existe en el repo con el test de
regresión del bug de subpaquetes. Adaptarlo a `package ssr_test`:

1. Cambiar `package assetmin` → `package ssr_test`
2. Cambiar `import "github.com/tinywasm/assetmin"` → `import "github.com/tinywasm/ssr"`
3. El test llama `extractSSRAssetsForModule` directamente (era internal) — reemplazar por
   `e.ExtractModule(subDir)` donde `e = ssr.New(parentDir)` con `SetListModulesFn` mockeado.
4. Eliminar referencias a `ssrGlobalCache`, `newSSRCache`, `Module` (tipos internos movidos).

Agregar también en `tests/extract_test.go`:

```go
func TestExtractAll_Empty(t *testing.T) {
    root := t.TempDir()
    os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\ngo 1.24\n"), 0644)
    e := ssr.New(root)
    e.SetListModulesFn(func(string) ([]string, error) { return []string{root}, nil })
    all, err := e.ExtractAll()
    if err != nil { t.Fatal(err) }
    _ = all
}

func TestExtractModule_NoSSRFiles(t *testing.T) {
    root := t.TempDir()
    os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\ngo 1.24\n"), 0644)
    e := ssr.New(root)
    a, err := e.ExtractModule(root)
    if err != nil { t.Fatal(err) }
    if a != nil { t.Error("expected nil for module with no SSR files") }
}
```

---

## Paso 5: Limpiar

Eliminar los archivos `*_original.go` una vez que `invoke.go`, `extract.go`, `cache.go`,
`scanner.go` estén completos y los tests pasen.

---

## Verificación

```bash
cd tinywasm/ssr
go mod tidy
go build ./...
gotest
```
