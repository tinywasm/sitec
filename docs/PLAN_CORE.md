# PLAN_CORE: El reinicio vía MCP puede arrancar desde el directorio equivocado

Fecha: 2026-07-24
Estado: propuesto — repo `tinywasm/core` clonado y leído en esta sesión
(**solo lectura**, sin permisos de push ni rama asignada); este documento es
la especificación completa para implementarlo directamente en ese repo.

Repo: `tinywasm/core` (GitHub) — módulo Go `github.com/tinywasm/app`, paquete
`app`. Todas las rutas de archivo de este documento son relativas a la raíz
de ese repo, no de `tinywasm/ssr`.

**Bug independiente del de `docs/PLAN.md`.** No comparten causa, no comparten
código, no comparten fix. Coinciden solo en el síntoma superficial ("hay que
reiniciar y a veces el renderizado no es el esperado"). Este documento cubre
EXCLUSIVAMENTE el bug de la ruta de arranque; el bug de orden del CSS ya está
diagnosticado y resuelto en `docs/PLAN.md` de este mismo repo (`ssr`).

## 1. Síntoma reportado

Cita textual del reporte: al arrancar con `tinywasm -tui` desde un subpaquete
(p. ej. `myapp/layoutdemo`), y cuando posteriormente un LLM reinicia el
proyecto a través de MCP, el proyecto arranca desde la raíz `myapp/` en vez
de desde el subpaquete `myapp/layoutdemo` donde arrancó la primera vez —
mostrando otra interfaz (`web/client.go`) distinta a la que se inició al
principio.

## 2. Modelo mental necesario (arquitectura mínima, para no releer todo core)

- `tinywasm -tui` ejecuta `Bootstrap()` (`bootstrap.go`). Si el puerto del
  daemon (6060 por defecto) está libre, lanza el daemon como proceso de
  fondo (`startDaemonProcess`) y luego corre el TUI cliente (`runClient`).
- `runClient` (`bootstrap.go:90-188`) envía al daemon, por HTTP
  (`POST /tinywasm/action`), la acción `{"key":"start","value":cfg.StartDir}`
  — `cfg.StartDir` es el directorio EXACTO desde el que se invocó el binario
  (p. ej. `myapp/layoutdemo` si el usuario corrió `tinywasm -tui` ahí).
- El daemon (`daemon.go`) es un proceso GLOBAL, único, que sobrevive a
  cualquier invocación individual de `tinywasm -tui`. Mantiene un
  `daemonToolProvider` (`dtp`) con un campo `lastPath string` — "el último
  path usado para arrancar/reiniciar" (comentario en el propio código,
  `daemon.go:401`).
- `dtp.startProject(path)` (`daemon.go:672-732`) es el ÚNICO punto que
  sobrescribe `lastPath` (`d.lastPath = projectPath`, línea 691) y el único
  que realmente lanza `Start(path, ...)` (`start.go`) en una goroutine.
- `Start(startDir, ...)` (`start.go:35`) usa `startDir` **literalmente**:
  `h.RootDir = startDir` (línea 70), sin resolverlo al root del módulo. Ese
  `h.RootDir` es lo que después determina, entre otras cosas,
  `ssr.New(h.RootDir)` y `ReloadSSRModule(h.RootDir)`
  (`section-build.go:256,276`) — es decir, literalmente que vista
  (`web/client.go` de qué paquete) se sirve. Pasar `myapp/` en vez de
  `myapp/layoutdemo` sirve una app distinta, no la misma app con un CSS
  distinto.
- Hay DOS formas de "reiniciar" en el código, y son asimétricas:
  - `restartCurrentProject()` (`daemon.go:633-643`) — usa `d.lastPath`, NO
    requiere que el llamador sepa ningún path. Solo es alcanzable a través
    de la acción interna `"restart"` (`ActionRestart`), que llega por el
    endpoint HTTP `/tinywasm/action` o por el método JSON-RPC
    `tinywasm/action` — ambos pensados para el TUI/navegador propio de
    tinywasm, **no están expuestos como tool MCP**.
  - `start_development` (`daemon.go:489-517`) — la única tool MCP capaz de
    (re)iniciar un proyecto. Su argumento `project_path` es
    **obligatorio** (`model.go:35`: `NotNull: true`) en cada llamada. Llama
    `d.startProject(args.ProjectPath)` con LO QUE SEA que el llamador MCP
    pasó, sin comparar contra `lastPath` ni avisar si difiere.

## 3. Mecanismo confirmado (evidencia exacta)

1. El usuario corre `tinywasm -tui` en `myapp/layoutdemo`. Vía
   `runClient`/`sendAction("start", cfg.StartDir)`, el daemon ejecuta
   `startProject("myapp/layoutdemo")` → `lastPath = "myapp/layoutdemo"` →
   `Start("myapp/layoutdemo", ...)` → `h.RootDir = "myapp/layoutdemo"`. La
   UI que se sirve es la de ese subpaquete. **Correcto hasta aquí.**

2. Tiempo después, un LLM conectado por MCP decide reiniciar el proyecto
   (para recoger cambios de código, o para recuperarse de un estado
   colgado). La única tool MCP disponible para eso es `start_development`
   (`daemon.go:489-517`), y es **obligatorio** darle un `project_path`
   (`model.go:35`).

3. El LLM no tiene forma de CONSULTAR cuál es el path activo actualmente:
   - `app_info` (`daemon.go:564-590`, la única tool MCP de introspección)
     reporta URL, directorio público, modo de tamaño y módulos Go
     descubiertos — **nunca `h.RootDir`**, el único dato que de verdad
     distingue "raíz del módulo" de "subpaquete `layoutdemo`".
   - La descripción de la tool `start_development`
     (`"Start a TinyWASM project. Project tools are pre-registered..."`,
     `daemon.go:493`) no dice en ningún lado que `project_path` deba
     coincidir EXACTAMENTE con el directorio ya activo, ni qué pasa si no
     coincide.
   - `lastPath` sí tiene el valor correcto internamente, pero no se expone
     por ningún tool MCP — solo lo usa `restartCurrentProject`, inalcanzable
     desde MCP (ver arriba).

4. Sin esa información, el LLM pasa lo que le parece más natural como
   `project_path` — típicamente la raíz del módulo (`myapp/`), que es lo que
   ve al inspeccionar el repo, no el subpaquete concreto donde el usuario
   arrancó por primera vez.

5. `start_development` ejecuta `ResolveProjectRoot(args.ProjectPath)`
   (`project_path.go`) solo para VALIDAR que hay un `go.mod` en esa ruta o
   algún ancestro — `myapp/` lo cumple perfectamente, así que la validación
   pasa sin ningún aviso. Luego `d.startProject("myapp/")` sobrescribe
   `lastPath = "myapp/"` y relanza `Start("myapp/", ...)` →
   `h.RootDir = "myapp/"`. El subpaquete `layoutdemo` deja de servirse; se
   sirve lo que sea que haya en `web/client.go` de la raíz (u otra vista por
   defecto). **Esto reproduce exactamente el síntoma reportado**: "muestra
   otra interfaz, no la que se inició al principio" — sin ningún error, sin
   ningún log de advertencia, la petición MCP se completa con éxito.

Esto NO es un bug de concurrencia ni de mapas — es una tool MCP obligatoria
sin forma de decir "reutiliza lo que ya está corriendo", combinada con cero
observabilidad del path activo. Confirmado leyendo el código real, no
inferido.

## 4. Por qué "a veces" y no siempre

- Si el LLM vuelve a pasar el path idéntico (`myapp/layoutdemo`),
  `startProject` detecta `samePath && alive && d.devServerHealthy()`
  (`daemon.go:679`) y solo re-adjunta/recarga sin relanzar — el bug no se
  manifiesta.
- El bug solo aparece cuando el path que el LLM decide pasar difiere,
  literalmente como string, del `lastPath` real — lo cual depende de qué
  tanto contexto tiene el LLM sobre "desde dónde se abrió originalmente este
  proyecto" en cada sesión/turno. De ahí el "a veces": no es aleatoriedad del
  runtime, es variabilidad de qué path decide pasar cada vez quien llama a
  la tool MCP.

## 5. Plan de corrección — un solo camino

Tres cambios, los tres en `daemon.go` salvo donde se indique, aditivos (no
tocan el comportamiento existente de `start_development` cuando el path
coincide). Implementar en este orden.

### 5.1 Nueva tool MCP `restart_development` (fix principal)

Da al LLM una forma de reiniciar SIN tener que adivinar ni recordar el path.

**a)** Cambiar la firma de `restartCurrentProject` para que devuelva el
estado (hoy no devuelve nada; los dos call-sites existentes —
`daemon.go:252` y `daemon.go:326`— siguen compilando igual porque llamar a
una función y descartar su único valor de retorno es válido como
statement):

```go
// restartCurrentProject restarts the project at lastPath, reusing the exact
// directory it was originally started from. Returns a short status string
// (mirrors startProject's return) for callers that want to surface it.
func (d *daemonToolProvider) restartCurrentProject() string {
	d.mu.Lock()
	path := d.lastPath
	d.mu.Unlock()

	if path == "" {
		d.logger("Cannot restart: no project has been started yet.")
		return "no active project — call start_development with a project_path first"
	}
	return d.startProject(path)
}
```

**b)** Añadir el método `Execute` (mismo patrón que `ExecuteGetLogs` /
`ExecuteAppInfo`, cerca de ellos):

```go
// ExecuteRestartDevelopment restarts the active project from the exact
// directory it was originally started from. Takes no arguments on purpose:
// a caller that has to re-supply a path can get it wrong (e.g. pass the
// module root when a subpackage was actually running) and silently switch
// which view is served — see docs/PLAN_CORE.md in tinywasm/ssr.
func (d *daemonToolProvider) ExecuteRestartDevelopment(ctx *context.Context, req mcp.Request) (*mcp.Result, error) {
	status := d.restartCurrentProject()
	return mcp.Text("Development environment: " + status), nil
}
```

**c)** Registrar la tool en `Tools()` (`daemon.go:489-535`), como tercer
elemento del slice, junto a las otras dos `project`:

```go
{
	Name:        "restart_development",
	Description: "Restart the active TinyWASM project from the exact directory it was originally started from. Takes no arguments. Prefer this over start_development when you just need to pick up code changes or recover a stuck project — re-calling start_development with a re-guessed project_path can silently switch to a different subpackage and serve a different UI.",
	Resource:    "project",
	Action:      'u',
	Execute:     d.ExecuteRestartDevelopment,
},
```

`Action: 'u'` sigue el mismo convenio de literal-byte que ya usan `'c'`
(`start_development`) y `'r'` (`app_get_logs`, `app_info`) en este mismo
archivo — `'u'` porque reinicia (actualiza el estado de) el proyecto activo,
no crea uno nuevo ni solo lee. No hay que resolver ni justificar más que
esto: es el mismo patrón ya usado dos líneas arriba, extendido a la letra
que falta.

No se declara `Args` (se omite el campo, igual que en `app_info`,
`daemon.go:528-533`): `mcp.Tool.Args` es `model.Fielder`, y `nil` significa
"sin argumentos" (comentario en `tinywasm/mcp@v0.2.4/tools.go:20`). Es
DELIBERADO que esta tool no tenga forma de recibir un path — esa es la
protección en sí.

### 5.2 `app_info` debe reportar el path activo (observabilidad)

Para que incluso si algún cliente MCP no usa `restart_development` y decide
llamar `start_development` de nuevo, tenga forma de CONSULTAR antes cuál es
el path correcto.

En `ExecuteAppInfo` (`daemon.go:564-590`), añadir una línea al `strings.Builder`,
como PRIMER campo tras el encabezado (antes de URL, para que sea lo primero
que un LLM lea):

```go
var sb strings.Builder
sb.WriteString("TinyWasm Project Info:\n")
sb.WriteString(fmt.Sprintf("- Active Path: %s\n", h.RootDir)) // NUEVO
sb.WriteString(fmt.Sprintf("- URL: http://localhost:%s\n", h.Config.ServerPort()))
// ... resto sin cambios
```

### 5.3 Aviso explícito en `startProject` cuando el path cambia bajo un proyecto vivo

Última capa, puramente diagnóstica (no bloquea nada, no cambia el
comportamiento) — deja rastro en los logs si, pese a 5.1 y 5.2, algo sigue
pasando un path distinto al activo.

En `startProject` (`daemon.go:672-732`), justo antes de `d.lastPath =
projectPath` (línea 691, dentro del segundo `d.mu.Lock(); defer
d.mu.Unlock()`), insertar:

```go
d.mu.Lock()
defer d.mu.Unlock()

if alive && !samePath && d.lastPath != "" {
	d.logger("start_development: switching active project from", d.lastPath, "to", projectPath,
		"— this changes which subpackage/UI is served. To restart the SAME project instead, use restart_development.")
}

d.lastPath = projectPath
```

(`alive` y `samePath` ya están calculados más arriba en la misma función,
bajo el primer lock — no hace falta recalcular nada, solo leer `d.lastPath`
aquí, que en este punto todavía tiene el valor VIEJO porque la línea
siguiente es la que lo sobrescribe.)

### 5.4 Texto de la tool `start_development` (aclaración, no código)

Añadir una frase a la `Description` existente (`daemon.go:493`) para que un
LLM que sí necesite arrancar algo nuevo entienda la semántica exacta:

```
"Start a TinyWASM project. Project tools are pre-registered; they return a 'not ready' error until the project is linked. project_path must be the EXACT directory to activate (the project root or a direct subpackage) — to restart the project that is ALREADY running, use restart_development instead of re-supplying this path; passing a different literal path here switches to a different subpackage/UI with no warning."
```

## 6. Qué NO hacer (para no divergir de este plan)

- **No** hacer `project_path` opcional en `start_development` (p. ej. "si se
  omite, reutiliza `lastPath`"). Cambiaría la validación existente
  (`NotNull: true`) de una tool que otros clientes ya pueden estar llamando
  siempre con el argumento presente, y requeriría que el LLM decida
  correctamente OMITIR el campo — un detalle tan fácil de errar como el que
  causa el bug hoy. La tool nueva y explícita de 5.1 es más segura porque no
  existe ningún campo por el que un path pueda colarse.
- **No** intentar resolver esto "adivinando" en el servidor si dos paths
  distintos son "el mismo proyecto" (comparar por módulo Go en vez de por
  directorio exacto, etc.) — perdería precisamente la distinción que importa
  (raíz vs. subpaquete = vista distinta). El path exacto es la unidad
  correcta aquí, no el módulo.
- **No** tocar `ResolveProjectRoot` (`project_path.go`) — su contrato actual
  (aceptar tanto la raíz como un subpaquete directo, devolviendo el módulo
  dueño solo para fines de validación/logging) es correcto y ya tiene tests
  (`project_path_test.go`) que no hay que romper. El bug no está ahí.

## 7. Tests a añadir

### 7.1 `tests/restart_development_tool_test.go` (paquete externo `test`, mismo estilo que `tests/daemon_logs_test.go`)

```go
package test

import (
	"strings"
	"testing"

	"github.com/tinywasm/app"
	"github.com/tinywasm/context"
	"github.com/tinywasm/mcp"
)

func findTool(tools []mcp.Tool, name string) (mcp.Tool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return mcp.Tool{}, false
}

// TestRestartDevelopmentTool_Exists is the direct regression test for the
// reported bug: there must be an MCP-reachable way to restart the active
// project that does NOT require the caller to supply (and potentially
// mis-supply) a project_path.
func TestRestartDevelopmentTool_Exists(t *testing.T) {
	dtp := app.NewDaemonToolProvider(app.BootstrapConfig{}, func(m ...any) {})
	tool, ok := findTool(dtp.Tools(), "restart_development")
	if !ok {
		t.Fatal("expected a restart_development MCP tool so a caller can restart the active project without re-guessing project_path")
	}
	if tool.Args != nil {
		t.Errorf("restart_development must take no arguments (it must always reuse the path the project was originally started from), got Args: %#v", tool.Args)
	}
}

func TestRestartDevelopmentTool_NoActiveProject(t *testing.T) {
	dtp := app.NewDaemonToolProvider(app.BootstrapConfig{}, func(m ...any) {})
	tool, ok := findTool(dtp.Tools(), "restart_development")
	if !ok {
		t.Fatal("restart_development tool not registered")
	}

	result, err := tool.Execute(context.Background(), mcp.Request{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(result.Content), "no active project") {
		t.Errorf("expected a clear 'no active project' message, got: %s", result.Content)
	}
}
```

### 7.2 `restart_active_path_test.go` (paquete interno `app`, raíz del repo, mismo estilo que `bug_tui_contamination_test.go`)

```go
package app

import (
	"os"
	"strings"
	"testing"

	"github.com/tinywasm/context"
	"github.com/tinywasm/mcp"
)

// TestExecuteAppInfo_ReportsActivePath reproduces the observability gap
// behind the "MCP restart lands on the wrong subpackage" bug: a caller has
// no way to look up which directory the active project is actually running
// from before deciding what path to pass to start_development, or whether
// to call restart_development instead. See docs/PLAN_CORE.md in
// tinywasm/ssr for the full trace.
func TestExecuteAppInfo_ReportsActivePath(t *testing.T) {
	h, tmpDir := setupHandler(t) // helper already defined in external_mode_flush_test.go
	defer os.RemoveAll(tmpDir)

	d := &daemonToolProvider{logger: func(args ...any) {}}
	d.mu.Lock()
	d.activeHandler = h
	d.mu.Unlock()

	result, err := d.ExecuteAppInfo(context.Background(), mcp.Request{})
	if err != nil {
		t.Fatalf("ExecuteAppInfo: %v", err)
	}
	if !strings.Contains(string(result.Content), h.RootDir) {
		t.Errorf("app_info must report the active project's RootDir so callers can restart the SAME directory instead of guessing.\ngot: %s", result.Content)
	}
}
```

Nota sobre 7.2: reutiliza deliberadamente `setupHandler(t)`
(`external_mode_flush_test.go:54-82`), que ya construye un `*Handler` con
`Config`/`WasmClient`/etc. no-nil vía `h.InitBuildHandlers()` — evita
duplicar esa construcción y evita el panic por nil-interface que resultaría
de armar un `&Handler{}` a mano (`ExecuteAppInfo` llama
`h.WasmClient.CurrentSizeMode()` sin nil-check). No uses `setActiveHandler(h)`
(el método real) en este test: internamente toca `d.ormProvider`, que
`NewDaemonToolProvider` no inicializa — por eso el test asigna
`d.activeHandler` directamente bajo `d.mu`, igual que ya hace
`bug_tui_contamination_test.go` con `d.projectTui`.

**Aviso honesto**: el sandbox donde se escribió este plan no tiene acceso a
dos dependencias privadas de `tinywasm/core` (`devtui`, `devwatch`), así que
NINGUNO de los tests de esta sección 7 pudo compilarse ni ejecutarse en esta
sesión — están derivados por lectura exacta del código y de los tests
hermanos ya existentes (mismos helpers, mismos patrones de construcción),
pero no verificados en rojo/verde. Al implementar: corre primero
`go test ./... -run 'RestartDevelopment|ActivePath'` ANTES del fix (deben
fallar — `restart_development` no existe todavía, `app_info` no reporta
`Active Path` todavía) y de nuevo DESPUÉS (deben pasar), siguiendo el mismo
método "rojo primero" que ya usa este repo (ver `docs/BUG.md`).

## 8. Criterios de aceptación

1. `TestRestartDevelopmentTool_Exists` — pasa (tool registrada, sin `Args`).
2. `TestRestartDevelopmentTool_NoActiveProject` — pasa.
3. `TestExecuteAppInfo_ReportsActivePath` — pasa.
4. Suite completa existente de `core` (`go test ./...` y `go test
   ./tests/...`) — sigue en verde, en particular `project_path_test.go`,
   `daemon_test.go`, `tests/daemon_logs_test.go`, `tests/mcp_daemon_test.go`,
   `bug_tui_contamination_test.go` (ningún cambio de este plan toca las rutas
   que esos tests ejercitan).
5. Manual (opcional, si hay entorno para correrlo): arrancar
   `tinywasm -tui` en un subpaquete, confirmar por `app_info` que "Active
   Path" muestra ese subpaquete, y confirmar que llamar `restart_development`
   sin argumentos mantiene la misma UI servida.
