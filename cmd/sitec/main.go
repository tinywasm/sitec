package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tinywasm/sitec"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	cmd := os.Args[1]
	switch cmd {
	case "build":
		runBuild(os.Args[2:])
	case "check":
		runCheck(os.Args[2:])
	case "help", "-h", "--help":
		printHelp()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Error: desconocido comando %q\n\n", cmd)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println("sitec — compilador de sitio con responsabilidad única")
	fmt.Println()
	fmt.Println("Uso:")
	fmt.Println("  sitec <comando> [opciones]")
	fmt.Println()
	fmt.Println("Comandos:")
	fmt.Println("  build [-o dir]   Corre el pipeline completo y escribe la salida")
	fmt.Println("  check            Valida la extracción sin escribir nada")
	fmt.Println("  help             Muestra esta ayuda")
	fmt.Println()
	fmt.Println("Opciones para build:")
	fmt.Println("  -o dir           Directorio de salida (por defecto: web/public)")
}

type Manifest struct {
	Status    string     `json:"status"`
	Command   string     `json:"command"`
	Artifacts []Artifact `json:"artifacts"`
}

type Artifact struct {
	Path      string `json:"path"`
	Mediatype string `json:"mediatype"`
}

func runBuild(args []string) {
	fsSet := flag.NewFlagSet("build", flag.ExitOnError)
	outputDir := fsSet.String("o", "web/public", "Directorio de salida")
	_ = fsSet.Parse(args)

	// Validate project
	if err := sitec.ValidateProject("."); err != nil {
		fmt.Fprintf(os.Stderr, "Error de validación del proyecto: %v\n", err)
		os.Exit(1)
	}

	e := sitec.New(".")
	e.SetLog(func(msg ...any) {
		fmt.Fprintln(os.Stderr, msg...)
	})

	// Check if web/client.go exists to decide if we compile WASM
	if _, err := os.Stat("web/client.go"); err == nil {
		e.SetWasmBuilder(sitec.NewDefaultWasmBuilder(false))
	}

	all, err := e.ExtractAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error de extracción: %v\n", err)
		os.Exit(1)
	}

	// Setup AssetMin with osFS
	am := sitec.NewAssetMin(&sitec.Config{
		OutputDir: *outputDir,
		RootDir:   ".",
	})
	am.SetLog(func(msg ...any) {
		fmt.Fprintln(os.Stderr, msg...)
	})

	am.SetSSRExtractor(e)
	am.SetFS(sitec.NewOsFS())

	if e.WasmBuilder() != nil {
		wasmOut, err := e.WasmBuilder().Build(".")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error building WASM: %v\n", err)
			os.Exit(1)
		}
		am.SetWasm(wasmOut.Filename, wasmOut.Runtime)
		if err := am.Write(wasmOut.Filename, wasmOut.Binary, "application/wasm"); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing WASM: %v\n", err)
			os.Exit(1)
		}
	}

	if err := am.RouteExtractedAssets(all); err != nil {
		fmt.Fprintf(os.Stderr, "Error ruteando assets: %v\n", err)
		os.Exit(1)
	}

	if err := am.FlushToDisk(); err != nil {
		fmt.Fprintf(os.Stderr, "Error guardando assets a disco: %v\n", err)
		os.Exit(1)
	}

	// Prepare manifest
	var artifacts []Artifact
	for _, art := range am.List() {
		artifacts = append(artifacts, Artifact{
			Path:      art.Path,
			Mediatype: art.Mediatype,
		})
	}

	manifest := Manifest{
		Status:    "success",
		Command:   "build",
		Artifacts: artifacts,
	}

	jsonBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando manifiesto: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(jsonBytes))
	os.Exit(0)
}

func runCheck(args []string) {
	// Validate project
	if err := sitec.ValidateProject("."); err != nil {
		fmt.Fprintf(os.Stderr, "Error de validación del proyecto: %v\n", err)
		os.Exit(1)
	}

	e := sitec.New(".")
	e.SetLog(func(msg ...any) {
		fmt.Fprintln(os.Stderr, msg...)
	})

	all, err := e.ExtractAll()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error de validación (check falló): %v\n", err)
		os.Exit(1)
	}

	// Prepare manifest (just list module names extracted)
	var artifacts []Artifact
	for _, a := range all {
		artifacts = append(artifacts, Artifact{
			Path:      a.ModuleName,
			Mediatype: "package/go",
		})
	}

	manifest := Manifest{
		Status:    "success",
		Command:   "check",
		Artifacts: artifacts,
	}

	jsonBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generando manifiesto: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(string(jsonBytes))
	os.Exit(0)
}
