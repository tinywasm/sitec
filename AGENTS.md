# AGENTS.md — `tinywasm/sitec`

Instrucciones obligatorias para cualquier agente que trabaje en este repositorio.

## Qué es este repo

`sitec` es el **compilador de sitio**: toma un árbol de fuentes Go y produce la
superficie estática desplegable — hoja de estilos, bundle de scripts, sprite SVG,
declaración de fuentes y shell HTML. Corre hasta terminar y sale.

Es un compilador, no un servidor ni un renderizador. El nombre sigue la
convención del ecosistema para compiladores: `ormc`, `ddlc`, `sitec`.

Se llamaba `tinywasm/ssr`. Ese nombre describía una técnica que la librería no
implementa: nada se renderiza server-side por petición; los productores corren
una vez en build y el resultado es estático.

## Los tests van TODOS en `tests/`

```
✅  tests/emit_fonts_test.go
✅  tests/serve_http_test.go
✅  tests/extract_test.go

❌  fonts_test.go              (en la raíz)
❌  tests_emit/fonts_test.go   (otra carpeta)
❌  serve/serve_test.go        (junto al código)
```

**Regla:** ni un solo `*_test.go` fuera de `tests/`. Verificable:

```sh
find . -name "*_test.go" -not -path "./tests/*" -not -path "./.git/*"
# debe devolver vacío
```

### Para agrupar, prefijos — no carpetas

`tests/` es un directorio plano. Los tests de una misma área se agrupan con un
**prefijo en el nombre del archivo**, que es suficiente para ordenarlos
alfabéticamente y encontrarlos:

| Prefijo | Área |
|---|---|
| `emit_` | etapa `emit`: ensamblado, minificación, escritura |
| `serve_` | subpaquete `serve`: exposición HTTP en modo desarrollo |
| `extract_` | etapa `extract`: ejecución de los productores |
| *(sin prefijo)* | pipeline y comportamiento general |

Crear una subcarpeta dentro de `tests/` para "ordenar" está prohibido: fragmenta
el paquete, obliga a duplicar los helpers y hace que `go test ./tests/` deje de
cubrir todo.

### Un solo paquete: `sitec_test`

Todos los archivos de `tests/` declaran `package sitec_test`. Go exige un único
paquete por directorio, y las pruebas son externas a propósito: ejercitan la API
pública, que es la que consumen `app` y `goflare`.

Si necesitas probar algo no exportado, **exponerlo o replantear el diseño** es la
respuesta; no lo es crear un test interno en la raíz.

### `testdata/`

Los fixtures viven en `tests/testdata/`. Go ignora ese nombre al compilar, así
que no interfiere con la regla anterior.

## Reglas de código

### Este repo es herramienta de backend

`sitec` corre en la máquina del desarrollador y en CI, y maneja el toolchain de
Go. Usa legítimamente la biblioteca estándar: `os`, `os/exec`, `encoding/json`,
`go/ast`, `go/parser`, `sync`, `io`.

**La regla del ecosistema de "nada de biblioteca estándar" NO aplica aquí** — esa
regla es para código que se compila a WASM. No "arregles" esos imports.

Para construir errores usa `github.com/tinywasm/fmt` (`fmt.Err`), que es la
convención del ecosistema.

### Sin strings hardcodeados

Todo string repetido —ruta por defecto, prefijo de log, mensaje de error, nombre
de flag— es una constante con nombre en el paquete. Los literales están
prohibidos en la lógica.

### Sin carpetas `internal/`

En este ecosistema una carpeta `internal/` señala un fork o una duplicación de
una dependencia en vez de contribuir aguas arriba.

### `cmd/` delgado

`cmd/sitec/main.go` contiene **solo**: parseo de flags, inyección de
dependencias, e imprimir/salir. Toda decisión es una función exportada de la
librería, y por tanto testeable desde `tests/`.

```go
// ❌ prohibido: lógica dentro de cmd/
func isProjectValid() bool { ... }

// ✅ correcto: exportado en la librería
func ValidateProject(dir string) error
```

### Contrato de ejecución del CLI

Lo van a manejar un runner de CI y un LLM:

- **Sin argumentos → ayuda por stdout, exit `0`.** Nunca bloquear en stdin ni en
  una TUI.
- **stdout = solo datos** (manifiesto JSON); **stderr = todos los logs**.
- Exit `0` en éxito y en ayuda; distinto de cero ante flags inválidos o fallo del
  pipeline.

## Invariantes que no puedes romper

Salieron de bugs reales, cada uno con su test. Si un cambio tuyo los toca, el
test debe seguir en verde:

1. **El alcance es la unión de los grafos de compilación** nativo y `js/wasm`.
   Filtrar por un solo `GOOS` pierde en silencio el CSS de componentes que solo
   importa el cliente.
2. **El alcance se ancla en el directorio de arranque**, no en la raíz del
   módulo. El arnés se arranca desde la raíz de lo que se prueba, que rara vez es
   donde está el `go.mod`.
3. **Un directorio `package main` nunca es un paquete del compilador**, en
   cualquier módulo y con cualquier nombre de carpeta.
4. **Si el sondeo del grafo falla, no se filtra.** Degradar abierto, nunca
   cerrado. "Vacío" y "desconocido" son valores distintos.
5. **Una extracción vacía es un fallo**, nunca un éxito silencioso: significa que
   la aplicación se serviría sin CSS.
6. **El modo por defecto es memoria.** Probar un componente no debe dejar un solo
   archivo en disco. Comprobable: tras `tinywasm -tui` en un componente,
   `git status` de su repo queda limpio.

## Plan vigente

[docs/PLAN.md](docs/PLAN.md). En cola:
[docs/PLAN_HTML_EN_APP.md](docs/PLAN_HTML_EN_APP.md).
