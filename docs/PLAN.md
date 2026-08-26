---
PLAN: "fix!: WasmBuilder compila el paquete, no un archivo suelto"
EXECUTOR: jules
REVIEWER: none
---

> This plan is dispatched via the CodeJob workflow. See skill: agents-workflow.

# PLAN — el builder de wasm pierde los archivos hermanos del entry point

## El problema, reproducido

`goflare-demo` tiene `edge/main.go` y `edge/access.go`, ambos `package main` con
el tag `wasm`. El build de goflare —que usa este paquete— falla así:

```
main.go:24:12: undefined: requireToken
main.go:46:41: undefined: authn
main.go:46:59: undefined: authorize
```

`requireToken`, `authn` y `authorize` están definidos en `access.go`, en el
mismo directorio, mismo paquete. El error es real: el compilador nunca ve ese
archivo.

Aislado, sin goflare ni TinyGo de por medio, el mismo bug con `go build` puro:

```sh
mkdir -p /tmp/repro/edge
cat > /tmp/repro/edge/main.go <<'EOF'
package main
func main() { helper() }
EOF
cat > /tmp/repro/edge/access.go <<'EOF'
package main
func helper() {}
EOF

cd /tmp/repro/edge && go build -o /tmp/out main.go
# ./main.go:3:15: undefined: helper

cd /tmp/repro/edge && go build -o /tmp/out .
# compila limpio
```

**La causa está fuera de goflare, aquí, en
[`wasm_builder_exec.go`](../wasm_builder_exec.go).** `Build` invoca al
compilador pasándole el **nombre del archivo de entrada** como argumento
posicional:

```go
cmd = exec.Command("tinygo", "build", "-target", "wasm", "-no-debug", "-o", tmpOutPath, entry)
// ó, en modo stdlib:
cmd = exec.Command("go", "build", "-o", tmpOutPath, entry)
```

Cuando el argumento de `go build`/`tinygo build` es un archivo (termina en
`.go`), el compilador entra en **modo "command-line-arguments"**: compila
exactamente los archivos listados, nunca los demás del directorio, aunque sean
el mismo paquete. Es un modo de Go pensado para "compila este archivo suelto",
no para "compila este paquete". Pasarle un solo archivo cuando el paquete tiene
varios es, precisamente, el bug.

## Por qué no se vio antes

Todos los tests de este builder —y los tres proyectos que lo consumen hoy
(`goflare-demo`, `misitio`, `iam`)— usan un frontend (`web/client.go`) de un
solo archivo. `web/server.go`, cuando existe, lleva `//go:build !wasm` y por
eso el compilador ya lo excluye por su cuenta, sin que el bug se note. El
primer proyecto en dividir su **entry point del edge Worker** en más de un
archivo (`goflare-demo`, `edge/main.go` + `edge/access.go`) lo destapó.
Cualquier proyecto que crezca más allá de un `main.go` único lo va a repetir.

## El arreglo

En [`wasm_builder_exec.go`](../wasm_builder_exec.go), función `Build`: el
compilador debe apuntar al **directorio** que contiene el entry point, no al
archivo. `cmd.Dir` ya aísla el directorio correcto para el caso `main.go`
(`filepath.Dir("main.go") == "."`); lo que falta es generalizarlo también al
caso `web/client.go`, donde hoy `cmd.Dir` es la raíz del proyecto y el archivo
va con la subcarpeta por delante.

Cambia el `cmd.Dir` para que apunte siempre al directorio del paquete, y pasa
`"."` al compilador en vez de `entry`:

```go
func (w *defaultWasmBuilder) Build(dir string) (WasmOutput, error) {
	entry := w.opts.entry()
	clientPath := filepath.Join(dir, entry)
	if _, err := os.Stat(clientPath); err != nil {
		return WasmOutput{}, fmt.Err("input file not found: ", entry, " must exist")
	}
	pkgDir := filepath.Join(dir, filepath.Dir(entry))

	// ... (env sin cambios)

	var cmd *exec.Cmd
	if !w.stdlib {
		cmd = exec.Command("tinygo", "build", "-target", "wasm", "-no-debug", "-o", tmpOutPath, ".")
	} else {
		cmd = exec.Command("go", "build", "-o", tmpOutPath, ".")
	}
	cmd.Dir = pkgDir
	cmd.Env = env
	// ... resto sin cambios
```

La verificación de `os.Stat(clientPath)` **se queda igual**: sigue
comprobando que el archivo nombrado en `Entry` existe, que es lo que da el
mensaje de error legible ("input file not found: main.go"). Lo único que
cambia es contra qué se invoca el compilador.

### El comentario del campo `Entry` miente ahora — corrígelo

```go
// Entry is the input file, relative to the directory passed to Build.
// Empty means "web/client.go".
Entry string
```

Reescríbelo para que diga lo que de verdad pasa: `Entry` nombra el archivo que
debe existir y en qué directorio vive el paquete a compilar, pero **compila el
directorio entero como paquete**, no solo ese archivo.

## Por qué esta y no otra alternativa

- **Enumerar a mano los archivos hermanos y pasarlos todos como argumentos**:
  descartado. Habría que reimplementar la evaluación de build constraints que
  ya hace el propio compilador (`//go:build wasm`, `_test.go`, nombres
  `_GOOS.go`); dejarlo en manos de `go build`/`tinygo build` es lo correcto y
  lo que ya hacen implícitamente cuando se les pasa un directorio.
- **No tocar `sitec` y exigir por convención que el entry point del edge sea
  un único archivo**: descartado. No es una regla que el compilador imponga
  —nada avisa si se rompe—, y ya se rompió sola en cuanto un proyecto creció
  de forma natural. Es exactamente el footgun que este plan cierra.

## Criterios de aceptación

- `grep -n 'entry)' wasm_builder_exec.go` → vacío: ya no se pasa `entry` al
  compilador en ninguna rama.
- `gotest ./...` en verde, en los dos modos (`stdlib` y TinyGo).
- El repro de arriba (`edge/main.go` + `edge/access.go`, dos archivos,
  mismo paquete `main`) compila sin error.

## Test nuevo — en `tests/build_wasm_output_test.go`

Sigue el prefijo `build_` que ya usa este archivo para el builder de wasm.

```go
// TestWasmBuild_CompilesSiblingFilesInSamePackage cierra la regresion real de
// goflare-demo: un entry point con mas de un archivo en el mismo paquete
// perdia los archivos hermanos porque el compilador se invocaba con el
// nombre de archivo (modo command-line-arguments) en vez del directorio.
func TestWasmBuild_CompilesSiblingFilesInSamePackage(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(tmpDir, "edge"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "edge", "main.go"),
		[]byte("package main\nfunc main() { helper() }"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "edge", "access.go"),
		[]byte("package main\nfunc helper() {}"), 0644); err != nil {
		t.Fatal(err)
	}

	wb := sitec.NewWasmBuilder(true, sitec.WasmBuildOptions{
		Entry:      "edge/main.go",
		OutputName: "edge",
	})
	out, err := wb.Build(tmpDir)
	if err != nil {
		t.Fatalf("Build failed: %v", err)
	}
	if len(out.Binary) == 0 {
		t.Error("Binary is empty")
	}
}
```

Corre en modo `stdlib` (`true`) a propósito: no depende de TinyGo instalado y
reproduce el bug igual de bien, porque el bug está en qué argumento recibe el
compilador, no en cuál de los dos compiladores es.

## Documentación

- El doc comment de `Entry` en `wasm_builder_exec.go` (ya cubierto arriba).
- **No** hace falta tocar `docs/ARCHITECTURE.md`: esto no cambia el
  comportamiento observable para nadie que ya tuviera un entry point de un
  solo archivo, solo arregla el caso que hoy falla.
- **No** referencies este plan desde ningún documento permanente: `codejob`
  borra `docs/PLAN.md` al publicar y la referencia queda muerta.

## Lo que NO hay que hacer

- **No** toques `emit_*.go` ni el pipeline de `extract`/`select`/`reach`: son
  la etapa de compilación del **frontend estático**, no del wasm builder. Este
  plan es solo `wasm_builder_exec.go` y su test.
- **No** cambies la firma de `Build`, `NewWasmBuilder` ni `NewDefaultWasmBuilder`.
  Los tres consumidores (`goflare`, y por extensión `misitio`/`iam`/`goflare-demo`
  a través de él) dependen de esa firma exacta.
- **No** toques `tinywasm/goflare`. Su plan más reciente prohibió explícitamente
  tocar `sitec` desde allí, precisamente porque el arreglo pertenece aquí.
