# PLAN: tinywasm/ssr — Extractor de assets SSR (delegado desde assetmin)

## Repositorio
`github.com/tinywasm/ssr` — path local: `tinywasm/ssr/` (creado con gonew, v0.0.1)

Estado actual: stub `type Ssr struct{}` + `func New() *Ssr` (a reemplazar).

## Dependencias de ejecución
```bash
go install github.com/tinywasm/devflow/cmd/gotest@latest
```

---

## Propósito

assetmin mezclaba dos roles: **bundler/minifier** (su nombre) y **extractor SSR** (codegen +
`go run` sobre el proyecto). Este paquete absorbe el **extractor**, dejando assetmin como bundler
puro. Mismo patrón que `image/min`: la extracción/procesamiento vive fuera; assetmin recibe el
contenido.

`tinywasm/ssr` descubre los módulos de componentes, ejecuta sus métodos `Render*` en build-time
(generando un `main.go` temporal y corriéndolo), y devuelve los assets por módulo. **No** hace
bundling ni minificación.

---

## Contrato (evita ciclo de dependencias)

**assetmin define** la interfaz y el DTO (es el consumidor → define el contrato).
**ssr implementa** la interfaz y devuelve el DTO (importa assetmin).
**assetmin NO importa ssr** → sigue liviano (sin `os/exec` ni toolchain en su código).
**app inyecta** — simétrico con `ImageProcessor`.

En assetmin (ver assetmin PLAN Cambio 7):
```go
type SSRAssets struct {
    ModuleName  string
    RootCSS     string
    CSS         string
    JS          []*js.Script
    HTML        string
    Icons       *svg.Sprite   // ← antes map[string]string
    IsRoot      bool
    IsFramework bool
}

type SSRExtractor interface {
    ExtractModule(moduleDir string) (*SSRAssets, error)  // hot reload de un módulo
    ExtractAll() ([]*SSRAssets, error)                   // escaneo completo (descubre módulos)
}

func (c *AssetMin) SetSSRExtractor(e SSRExtractor)
```

ssr implementa `SSRExtractor` devolviendo `*assetmin.SSRAssets`. El routing (slots, RootCSS
single-winner) **se queda en assetmin** (`routeAssets`, `resolveAndApplyRootCSS`); ssr solo
extrae datos crudos por módulo.

---

## Qué se mueve desde assetmin

| Archivo en assetmin | Destino en ssr | Contenido |
|---------------------|----------------|-----------|
| `ssr_invoke.go` | `invoke.go` | codegen del `main.go`, `go run`, parseo JSON, regex `Render*` |
| `ssr_extract.go` | `extract.go` | descubrimiento de módulos (`go list -m`), extracción por módulo |
| `ssr_loader.go` (parte) | `loader.go` | `ExtractAll` (orquestación del escaneo). **NO** `routeAssets`/`resolveAndApplyRootCSS` (quedan en assetmin) |
| `ssr_cache.go` | `cache.go` | caché md5 de resultados de extracción (skip `go run` si nada cambió) |
| `import_scanner.go` | (ver Paso 4) | scanning de imports → **delegar a `tinywasm/depfind`** si la API alcanza |

> `ScheduleSSRLoad`/`WaitForSSRLoad`/`LoadSSRModules` (orquestación async + routing) **se quedan
> en assetmin**, pero su núcleo de extracción llama a `ssrExtractor.ExtractAll()` / `ExtractModule()`.

---

## Paso 1: Reemplazar el stub

Eliminar `ssr.go` (stub `Ssr`/`New`). Crear el `Extractor`:

```go
package ssr

import "github.com/tinywasm/assetmin"

type Extractor struct {
    rootDir       string
    listModulesFn func(rootDir string) ([]string, error)
    log           func(...any)
    // caché, etc.
}

func New(rootDir string) *Extractor { return &Extractor{rootDir: rootDir, log: func(...any) {}} }

func (e *Extractor) SetLog(fn func(...any))                                   { e.log = fn }
func (e *Extractor) SetListModulesFn(fn func(string) ([]string, error))      { e.listModulesFn = fn }

// ExtractModule / ExtractAll implementan assetmin.SSRExtractor.
func (e *Extractor) ExtractModule(moduleDir string) (*assetmin.SSRAssets, error) { /* ... */ }
func (e *Extractor) ExtractAll() ([]*assetmin.SSRAssets, error)                  { /* ... */ }
```

---

## Paso 2: Mover el pipeline de extracción

Mover los archivos de la tabla, cambiando `package assetmin` → `package ssr`. Ajustar:
- El DTO de retorno pasa de `*SSRAssets` (local) a `*assetmin.SSRAssets` (importado).
- Las regex `reRenderCSS/reRenderJS/reRenderHTML/reIconSvg/reRootCSS` se mantienen.
- El template del `main.go` generado (`GenerateExtractorMain`) se mantiene, ajustando los
  imports del programa generado a los paths reales de los módulos.
- **`ExtractAll`/`ExtractModule` pueblan `IsRoot`/`IsFramework`** en el DTO: la lógica
  `isRootDir(m.Dir, rootDir)` y `isFramework := strings.Contains(m.Path, "tinywasm/css")`
  (hoy en `assetmin/ssr_loader.go:124-125`) **se mueve a ssr**. El `routeAssets` de assetmin
  solo **lee** esos flags. La constante `cssModulePath = "tinywasm/css"` y el helper `isRootDir`
  se mueven a ssr.
- `Icons` en el DTO y en `ssrCollectorOutput` (struct del programa generado) pasa de
  `map[string]string` a `*svg.Sprite` (requiere svg con JSON, Paso 3).

---

## Paso 3: Serialización del sprite a través del IPC (codegen)

⚠️ **Cross-cutting con el refactor de svg.** El extractor corre en **otro proceso** (`go run`)
y devuelve **JSON**. Hoy `Icons` es `map[string]string` (JSON trivial). Con `IconSvg() *svg.Sprite`
el sprite debe cruzar el límite de proceso.

**Requisito (añadir a `tinywasm/svg`):** `*svg.Sprite` debe ser JSON round-trip —
implementar `MarshalJSON`/`UnmarshalJSON` (o exportar un slice serializable de
`{id, body, viewBox}`). Así:
- El `main.go` generado hace `s.Icons = inst.IconSvg()` y serializa el sprite a JSON.
- ssr deserializa a `*svg.Sprite` y lo pone en `SSRAssets.Icons`.
- assetmin hace `masterSprite.Merge(a.Icons)` uniforme (mismo path que el in-process
  `RegisterComponents`).

> Sin esto, el path codegen y el in-process divergen. Anotar como dependencia del svg PLAN.

---

## Paso 4: Scanning de imports → `tinywasm/depfind`

`import_scanner.go` (AST + go list) duplica lo que ya hace `tinywasm/depfind`
(`GoDepFind`: análisis de dependencias, ownership, parseo de imports, caché).

- **Preferido:** usar `depfind` para descubrir qué módulos/subpaquetes están en uso.
- **Si hay gap de API** (depfind no expone un "ProjectImports(rootDir) map[string]bool"
  equivalente): agregar ese método exportado a `depfind`, o como último recurso mantener un
  scanner mínimo en ssr. Decidir al implementar (verificar la superficie de depfind primero).

go.mod de ssr:
```
require (
    github.com/tinywasm/assetmin v<nueva>
    github.com/tinywasm/css v<...>
    github.com/tinywasm/js v<...>
    github.com/tinywasm/svg v<...>
    github.com/tinywasm/depfind v<...>
    github.com/tinywasm/fmt v<...>
)
```

---

## Paso 5: Tests

Mover los tests de extracción de assetmin a `ssr/tests/`:
- `ssr_extract_subpackage_test.go` → `tests/extract_test.go` (`package ssr_test`).
  Recordar: los fixtures escriben `css.go`/`js.go` (no `ssr.go`, eliminado).
- Tests del codegen/invoke y del caché.

> Nota: aprovechar para verificar el fix de `/tmp` (ver memoria [[gotest-terminated-tmp-full]]):
> los tests que generan `main.go` temporales deben limpiar sus tmpdirs.

---

## Verificación

```bash
cd tinywasm/ssr
go mod tidy
go build ./...
gotest

cd tinywasm/assetmin && gotest
cd tinywasm/app && gotest
```

Ver `tinywasm/docs/MASTER_PLAN.md` para el orden global de ejecución.
