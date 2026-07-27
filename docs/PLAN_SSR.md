---
PLAN: "`tinywasm/ssr`: sustituir la detección por regex de nombre por una interfaz tipada"
---

> Depende de: `github.com/tinywasm/widget v0.1.0` (**ya publicado**), que expone
> `style.Styler` (`PLAN_WIDGET` §6).
> Es el plan más pequeño de los cuatro y el que más fallos silenciosos elimina.
>
> **Ejecución: un solo cambio, no por etapas.** `widget` ya está publicado y no queda ningún
> consumidor externo del escáner por regex fuera de este mismo árbol (`layout`, `components`),
> que se migran en la misma ventana de tiempo. No hay razón para mantener el escáner viejo
> vivo "por si acaso" mientras se prueba la interfaz nueva: se publican `Styler` y el nuevo
> `Collect` **y se retira el escáner en el mismo cambio**.

---

## El problema

De `AGENTS.md`, sección *"SSR asset provider names are matched by regex — the name must be
EXACT"*:

> *"`tinywasm/ssr` collects a package's SSR output by scanning `css.go`/`js.go`/`svg.go`/
> `html.go` for functions whose names match **exactly**: `RenderCSS`, `RootCSS`, `RenderHTML`,
> `RenderJS`, `IconSvg`. A CSS builder named anything else (e.g. `GenerateCSS`) is **silently
> never emitted** — the component renders with **zero styling** and nothing fails at build
> time."*

Y una segunda regla, tampoco expresada en ningún tipo:

> *"`ssr` requires all providers in a package to share ONE receiver (or all be free
> functions)… never mix a method with a free function, or receiver detection produces code
> that calls a method that doesn't exist."*

Esto es un contrato **por convención de nombres**, verificado por una expresión regular sobre
el texto fuente. Viola directamente el principio 6 (*fallar en compilación, nunca en
silencio*) y el 7 (*firmas auto-descriptivas*): la firma correcta no es descubrible por
autocompletado, y equivocarse produce un componente sin estilo que compila, arranca y se ve
mal en el navegador sin un solo error.

El síntoma está documentado hasta con su aspecto visual: *"a component renders unstyled while
its icons appear giant/black"*. Que exista una guía para reconocer el síntoma es la prueba de
que el defecto es estructural.

Peor aún: es la regla que **fuerza** la forma retorcida que hoy tiene el código. `AGENTS.md`
explica que `RenderCSS` debe declararse como **método** solo para no colisionar con la función
libre `RenderCSS` del paquete `css` importado con `.` — una restricción de diseño impuesta por
un detector de texto, no por una necesidad real.

---

## La solución: una interfaz

```go
// widget/style — ya definida en PLAN_WIDGET §6
type Styler interface {
	widget.Widget
	Style() *Sheet
}
```

`ssr` deja de escanear texto y **asevera la capacidad**, que es el patrón que la casa ya usa
en todas las demás costuras (`router.APIModule`, `view.Saver`, `view.Deleter`):

```go
// ssr — recolección
func Collect(parts ...widget.Widget) *Bundle {
	b := newBundle()
	for _, p := range parts {
		if s, ok := p.(style.Styler); ok {
			b.addSheet(s.Style())
		}
		if i, ok := p.(svg.IconProvider); ok {
			b.addIcons(i.Icons())
		}
		if h, ok := p.(HTMLProvider); ok {
			b.addHTML(h.HTML())
		}
	}
	return b
}
```

Lo que cambia, en concreto:

| Antes | Después |
|---|---|
| El nombre debe ser exactamente `RenderCSS` | El nombre lo fija la interfaz: `Style()` |
| Un nombre equivocado → CSS nunca emitido, sin error | Un nombre equivocado → **no satisface `Styler`** → error de compilación en el sitio de registro |
| Todos los proveedores del paquete comparten receptor | Irrelevante: son métodos de un tipo, no funciones sueltas |
| Método obligatorio solo por colisión con el `css` dot-importado | Ya no existe dot-import de `css` en los widgets |
| El detector lee el fuente | El detector no existe |

---

## El cambio, completo, de una vez

Un único commit hace las tres cosas — no hay versión intermedia que conviva con el escáner
viejo:

1. **Declarar las interfaces**: `Styler`, `HTMLProvider`, `JSProvider`, `IconProvider`.
2. **Reescribir `Collect`** para recibir `[]widget.Widget` y aseverar cada capacidad por tipo
   (el snippet de más arriba), en vez de escanear archivos fuente.
3. **Borrar el escáner de regex** en el mismo cambio: el archivo que matchea `RenderCSS`/
   `RootCSS`/`IconSvg` por nombre, la regla de "un solo receptor por paquete", y la sección de
   `AGENTS.md` *"SSR asset provider names are matched by regex"* completa —unas 25 líneas de
   reglas que el lector debía recordar— porque el tipo pasa a decirlas.

No hace falta un diagnóstico ruidoso transitorio para el camino viejo (una fase que le grite al
paquete que todavía usa `RenderCSS` sin `Styler`) porque no hay transición: `components` y
`layout` migran sus proveedores de estilo a `Styler` en la misma ventana de trabajo que este
cambio, así que cuando el escáner se borra ya no queda nadie que lo necesite. Si por secuencia
de commits `ssr` se publica un paso antes de que `components`/`layout` terminen, el build de
esos dos falla en compilación (no en runtime) hasta que migren su `RenderCSS` a `Style()` — que
es exactamente el fallo visible y accionable que el arnés prefiere sobre el silencioso que hay
hoy.

Es la reducción de documentación que el arnés promete: *"Because the API is the harness,
documentation shrinks to minimal 'how' instructions"*.

---

## Beneficio colateral: el orden de emisión deja de ser un accidente

Con `Collect` recibiendo `[]widget.Widget` en lugar de descubrir paquetes por escaneo, el
bundle puede ordenar la salida de forma determinista por capa
([`PLAN_WIDGET` §7](PLAN_WIDGET.md)):

```css
@layer tokens, primitives, widgets, states;
```

Hoy el orden de las hojas depende del orden de descubrimiento del escáner, que depende del
orden del sistema de archivos. Eso significa que **la cascada puede cambiar entre máquinas**.
Con capas explícitas, deja de importar: la capa manda sobre la especificidad y sobre el orden
de aparición.

---

## Criterios de aceptación

1. Un widget cuyo método de estilo se llame mal **no compila** en el punto de registro.
2. No queda escaneo de fuente en `ssr`; `grep -rn "RenderCSS\|IconSvg" ssr/` vacío salvo en el
   CHANGELOG.
3. La sección correspondiente de `AGENTS.md` está borrada, no reescrita.
4. El bundle emitido es idéntico byte a byte en dos ejecuciones y en dos máquinas.
5. Test en `ssr`: un tipo que satisface `Styler` emite; uno que solo tiene un `RenderCSS`
   suelto **no compila** al pasarse a `Collect` (test de compilación negativa).

---

## Contrato de Paquete SSR

Cada paquete que declare recursos SSR (`css.go`, `js.go`, `svg.go`, `html.go`) debe exportar una función denominada exactamente `SSR()` que no reciba argumentos y retorne una lista de widgets (`[]widget.Widget`):

```go
func SSR() []widget.Widget {
	return []widget.Widget{
		&MyComponent{},
	}
}
```

Esto permite al extractor invocar la recolección tipada mediante `ssr.Collect` sin necesidad de escaneo regex. Si falta esta función en un paquete con recursos SSR, el build fallará en tiempo de compilación.
