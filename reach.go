package sitec

import (
	"strings"
)

const skippedUnreachableFmt = "sitec: %d paquete(s) fuera del grafo de compilación fueron omitidos"

// reachSet es el conjunto de rutas de importación en el grafo de compilación
// del directorio de arranque, en todas las configuraciones para las que se
// compila el artefacto.
type reachSet map[string]bool

// GraphLister devuelve las rutas de importación transitivas de pattern para el
// GOOS/GOARCH dado. Se inyecta para que los tests no necesiten toolchain.
type GraphLister func(rootDir, pattern, goos, goarch string) ([]string, error)

// buildTargets son las configuraciones para las que se compila el artefacto.
// El conjunto alcanzable es su UNIÓN: un componente que solo importa el
// cliente WASM no aparece en el grafo del servidor, y descartarlo perdería sus
// estilos.
var buildTargets = []struct{ GOOS, GOARCH string }{
	{"", ""},       // nativo: el binario del servidor
	{"js", "wasm"}, // el cliente del navegador
}

type reachability struct {
	set   reachSet
	known bool // false => no filtrar
}

// goListDeps es la implementación por defecto de GraphLister
func goListDeps(rootDir, pattern, goos, goarch string) ([]string, error) {
	tc := NewExecToolchain()
	env := []string{}
	if goos != "" {
		env = append(env, "GOOS="+goos)
	}
	if goarch != "" {
		env = append(env, "GOARCH="+goarch)
	}

	var data []byte
	var err error
	if len(env) > 0 {
		data, err = tc.ListEnv(rootDir, env, "-e", "-deps", pattern)
	} else {
		data, err = tc.List(rootDir, "-e", "-deps", pattern)
	}
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out, nil
}

// computeReachability calcula la unión de paquetes alcanzables para todos los targets
func computeReachability(rootDir string, lister GraphLister) reachability {
	if lister == nil {
		lister = goListDeps
	}

	set := make(reachSet)
	anySuccess := false

	for _, target := range buildTargets {
		deps, err := lister(rootDir, "./...", target.GOOS, target.GOARCH)
		if err == nil {
			anySuccess = true
			for _, dep := range deps {
				set[dep] = true
			}
		}
	}

	if !anySuccess {
		return reachability{set: nil, known: false}
	}
	return reachability{set: set, known: true}
}
