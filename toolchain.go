package sitec

// Toolchain ejecuta el toolchain de Go. Todo go list / go run / go build del
// compilador pasa por este puerto, para que el cacheo, el manejo de GOOS y la
// clasificación de errores existan en exactamente un lugar.
type Toolchain interface {
	List(dir string, args ...string) ([]byte, error)
	ListEnv(dir string, env []string, args ...string) ([]byte, error)
	Run(dir string, args ...string) ([]byte, error)
}

// WasmBuilder produce el binario del cliente Y el runtime JS que lo carga.
// sitec decide sus rutas, sus nombres y su sink; CÓMO compilarlo (TinyGo vs Go,
// flags) es del adaptador.
type WasmBuilder interface {
	Build(dir string) (WasmOutput, error)
}

// WasmOutput son los DOS artefactos de una compilación. Van juntos a propósito:
// TinyGo y Go estándar emiten runtimes wasm_exec distintos, así que un binario
// servido con el loader del otro modo no arranca. Devolverlos por separado
// permitiría emparejarlos mal.
type WasmOutput struct {
	Binary   []byte // el .wasm
	Filename string // su nombre, que el shell HTML referencia
	Runtime  string // el glue JS correspondiente al modo usado
}
