---
PLAN: "feat: Fonts() como sexto productor, para que la tipografía del proyecto llegue a assetmin"
TAG: v0.2.0
---
## Antes de escribir código: lee [CONSTRUCTION_HARNESS.md](CONSTRUCTION_HARNESS.md)

**Es vinculante, no orientativo.**

| # | Principio | Cómo se aplica aquí |
|---|---|---|
| 1 | Typed over `any` | El productor devuelve `font.Declaration`, no un `string` parseado del AST. |
| 4 | One way to do each thing | Se añade un productor al conjunto que ya existe; no se inventa un segundo mecanismo de extracción. |
| 9 | Lego pieces, never forks | Sin esto, `assetmin` tendría que parsear Go — y ya hay una pieza que lo hace. |

**Prerrequisito:** `tinywasm/font` v0.1.0 publicado (`font/docs/PLAN.md`).
**Consumidor:** `assetmin/docs/PLAN.md`, que lee el campo nuevo. Publicar éste primero.

---

## 1. Por qué esto es de `ssr` y no de otro

Un proyecto declara su tipografía una vez:

```go
// config/fonts.go — sin build tag: la identidad cruza a WASM
func Fonts() font.Declaration {
    return font.Declare("Roboto", "config/fonts")
}
```

`assetmin` necesita ese valor para copiar las cuatro caras al directorio público y emitir
el `@font-face` con la URL correcta. Y no puede obtenerlo por ningún otro camino:

- **No parsea Go.** No hay un solo `go/parser` en ese repo; lo que necesita de código
  ajeno se lo inyectan (`SetSSRExtractor`, `SetImageProcessor`).
- **Nadie se lo puede pasar por `Config`.** `assetmin.Config` lo construye
  `tinywasm/app` (`app/section-build.go:74`), que **no importa el paquete `config` del
  proyecto**: `app` es el CLI que corre *dentro* del proyecto, y el proyecto es un
  directorio, no código que `app` compile contra sí mismo.

Leer un valor del código del proyecto tiene exactamente un mecanismo en este ecosistema, y
es éste: el programa que este módulo genera, compila y **ejecuta**, invocando los
productores del paquete. Por eso el valor que llega es una `font.Declaration` real y no
una cadena reconstruida a mano.

> Una versión anterior del máster plan afirmaba que «`ssr` no participa». Era falso: se
> descartó sobre la premisa de que el composition root importaba `config`, y no lo hace.

---

## 2. Cambio

### 2.1 Un sexto productor

`scanner.go:91-97` tiene el conjunto cerrado:

```go
producerNames := map[string]bool{
    "RootCSS": true, "RenderCSS": true, "RenderHTML": true, "RenderJS": true,
    "IconSvg": true,
    "Fonts":   true,   // ← nuevo
}
```

Y su rama en el `switch` de `invoke.go:342-354` (`case "Fonts": rf.HasFonts = true`), más
la línea correspondiente en la plantilla del programa generado (`invoke.go:180,190`),
donde `RootCSS` hace `s.Root += …`. Aquí:

```go
{{if .HasFonts}}s.Fonts = inst.Fonts(){{end}}
```

**Sin `.String()` ni conversión:** el valor viaja tipado hasta el DTO.

### 2.2 El campo en el DTO

`assetmin.SSRAssets` gana `Fonts font.Declaration` (lo declara `assetmin`, ver su plan) y
`extract.go:71-78` lo rellena:

```go
return &assetmin.SSRAssets{
    ModuleName: m.path,
    RootCSS:    output.Root,
    …
    Fonts:      output.Fonts,
}
```

### 2.3 Una sola declaración por módulo

`MergeResultsFor` fusiona el módulo y todos sus paquetes. CSS se **concatena**; una
tipografía no se concatena: si dos paquetes del mismo módulo declaran `Fonts()`, la
segunda no puede «sumarse» a la primera.

**Regla:** gana la primera en el orden estable que `MergeResultsFor` ya garantiza, y la
segunda produce un **error de extracción que nombra los dos paquetes**. No un aviso: dos
declaraciones significan que alguien cree que el producto tiene dos tipografías, y el
resultado silencioso sería que una de las dos se ignora sin que nadie lo sepa
(principio 6).

---

## 3. Lo que este plan NO hace

- **No copia archivos ni conoce `OutputDir`.** Este módulo extrae valores; los bytes son
  de `assetmin`.
- **No emite CSS.** El `@font-face` lo construye `css.FontFaces` (`css/docs/PLAN.md`).
- **No toca el flujo de `RootCSS()`.** El nombre de la familia dentro de `--font-sans`
  **ya funciona** hoy: `config/css.go` llama a `Fonts().Family()` y el valor viaja dentro
  del `RootCSS()` que este módulo ya extrae. Este plan añade el otro camino —el de los
  bytes—, no reemplaza aquél.
- **No decide cuándo se reextrae.** Ese enrutado (`fonts.go` en `ssrTextAssetFiles`) vive
  en `assetmin/ssr_watcher.go`.

---

## 4. Verificación

1. Un paquete con `func Fonts() font.Declaration` produce un `SSRAssets.Fonts` con la
   familia y el directorio declarados — comprobado con un proyecto real en `tests/`, no
   con un doble.
2. Un paquete **sin** `Fonts()` produce el cero-valor y ningún error. Es el caso mayoritario.
3. Dos paquetes del mismo módulo declarando `Fonts()` → error de extracción que nombra
   ambos.
4. `Fonts()` declarado sobre un receptor genérico falla con el mismo mensaje que ya dan
   los otros cinco productores (`invoke.go:332-336`), no con un panic.
5. Los cinco productores existentes siguen comportándose igual: el CSS extraído de un
   proyecto de prueba es byte a byte el de antes.
6. `gotest`.

`docs/` se actualiza en el mismo commit con el productor nuevo en la lista.
