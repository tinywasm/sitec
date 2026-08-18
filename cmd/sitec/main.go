package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

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
	outputDir := fsSet.String("o", sitec.DefaultOutputDir, "Directorio de salida")
	_ = fsSet.Parse(args)

	root, err := filepath.Abs(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolviendo el directorio: %v\n", err)
		os.Exit(1)
	}

	site, err := sitec.Build(sitec.BuildConfig{
		RootDir:   root,
		Mode:      sitec.ModeRelease,
		OutputDir: *outputDir,
		Log: func(msg ...any) {
			fmt.Fprintln(os.Stderr, msg...)
		},
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error en construcción: %v\n", err)
		os.Exit(1)
	}

	if err := site.WriteTo(sitec.NewOsFS()); err != nil {
		fmt.Fprintf(os.Stderr, "Error escribiendo sitio: %v\n", err)
		os.Exit(1)
	}

	var artifacts []Artifact
	for _, art := range site.Artifacts() {
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
	root, err := filepath.Abs(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolviendo el directorio: %v\n", err)
		os.Exit(1)
	}

	modules, err := sitec.Check(root, func(msg ...any) {
		fmt.Fprintln(os.Stderr, msg...)
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error de validación (check falló): %v\n", err)
		os.Exit(1)
	}

	var artifacts []Artifact
	for _, m := range modules {
		artifacts = append(artifacts, Artifact{
			Path:      m,
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
