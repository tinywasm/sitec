# sitec
<img src="docs/img/badges.svg">

Compilador de sitio: toma un árbol de fuentes Go y produce la superficie
estática desplegable del sitio — hoja de estilos, bundle de scripts, sprite SVG,
declaración de fuentes y shell HTML.

Corre hasta terminar y sale. Es un compilador, no un servidor ni un
renderizador — pensado para CI/CD tanto como para el arnés de desarrollo.

```
sitec              # ayuda, exit 0
sitec build -o dir # compila y escribe la salida
sitec check        # valida sin escribir nada (puerta de CI)
```

stdout entrega datos (manifiesto JSON); stderr entrega logs.

## Icono del sitio

Un proyecto declara su icono con `Favicon()` en su paquete de configuración (`!wasm`):

```go
//go:embed logo.png
var logo []byte

func (b *Brand) Favicon() favicon.Source {
    return favicon.Source{Raster: logo}
}
```

`sitec` deriva el juego completo vía `github.com/tinywasm/image/favicon` (`icon-32.png`, `icon-192.png`, `apple-touch-icon.png`, `favicon.ico` y `favicon.svg` si se provee SVG) y emite un `<link>` por cada archivo con `Rel`.

`sitec` **no sanea** el SVG que reciba: un SVG de un tercero se limpia antes con `github.com/tinywasm/svg/sanitize`. El de un proyecto es suyo y es de confianza.

## Estado

En construcción.
