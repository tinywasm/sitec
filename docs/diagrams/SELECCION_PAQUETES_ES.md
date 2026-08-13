# Selección de paquetes SSR — flujo y los dos casos de arranque

> Documento de verificación en español. El pipeline general está en
> [EXTRACTION.md](EXTRACTION.md) (inglés). Aquí solo interesa **qué paquetes
> entran al `main.go` generado** y por qué, según desde dónde se arranque.

## Premisa: se arranca desde la raíz de lo que se quiere probar

`tinywasm -tui` se ejecuta desde el directorio de lo que se prueba, **nunca**
desde su subcarpeta de cliente.

```
✅ cd components/calendarslider && tinywasm -tui
❌ cd components/calendarslider/web && tinywasm -tui
```

El directorio de arranque **no** es necesariamente la raíz del módulo.

---

## Flujo general

```mermaid
flowchart TD
    A["tinywasm -tui<br/>DIR_ARRANQUE"]
    B["modfind: go list -m -json all<br/>cmd.Dir = DIR_ARRANQUE"]
    C["módulo main = el que contiene go.mod<br/>⚠️ puede estar POR ENCIMA de DIR_ARRANQUE"]
    D["lista de módulos:<br/>main + dependencias en GOMODCACHE"]
    E["por cada módulo:<br/>WalkDir sobre TODA su raíz"]
    F{"reglas de selección<br/>(ver abajo)"}
    G["main.go generado<br/>importa cada paquete seleccionado"]
    H["go run main.go<br/>los productores se ejecutan"]
    I["JSON → merge por ruta → CSS servido"]

    A --> B --> C --> D --> E --> F --> G --> H --> I

    F -.->|descartado| X["no entra al main.go"]
```

---

## Reglas de selección

El orden importa: **primero se acota el alcance**, y solo dentro de ese alcance
se decide paquete por paquete.

```mermaid
flowchart TD
    Z["alcance = grafo de compilación<br/>alcanzable desde DIR_ARRANQUE"]
    Z1["union de DOS grafos:<br/>· go list -deps ./...  (GOOS nativo → servidor)<br/>· GOOS=js GOARCH=wasm ... (cliente)<br/>⚠️ filtrar por uno solo pierde CSS"]
    P["directorio candidato"]
    R0{"¿está en el alcance?"}
    S0["❌ NO seleccionar<br/>nadie lo importa → no aporta estilos<br/>· así cae calendarslider<br/>· así caen los 12 componentes no probados"]
    R1{"¿el paquete<br/>se llama main?"}
    S1["❌ NO seleccionar<br/>un package main no se puede importar<br/>· vale en CUALQUIER módulo<br/>· no depende del nombre del dir"]
    R2{"¿declara productor,<br/>tiene css.go, o importa<br/>una asset library?"}
    S2["⬜ no es un paquete SSR"]
    S6["✅ SELECCIONAR"]
    S7["✅ si NO compila, revienta<br/>es código alcanzable = código propio<br/>debe fallar fuerte, con 1 sola línea"]

    Z --> Z1 --> P --> R0
    R0 -->|no| S0
    R0 -->|sí| R1
    R1 -->|sí| S1
    R1 -->|no| R2
    R2 -->|no| S2
    R2 -->|sí| S6 --> S7
```

| Regla | Qué elimina |
|---|---|
| 1. Alcance (grafo de compilación) | calendarslider y todo lo que no se usa — **causa raíz** |
| 2. `package main` | demos y entry points, en cualquier módulo |
| 3. Sin productor | directorios que no aportan estilos |

Si el sondeo del grafo falla (sin toolchain), **no se filtra**: se degrada al
comportamiento actual en vez de servir una hoja vacía.

## Caso A — aplicación real

Arranque en `layout/platformd`. El `go.mod` está en `layout/`.

```mermaid
flowchart TD
    A["DIR_ARRANQUE = layout/platformd"]
    B["go.mod está en layout/<br/>módulo main = github.com/tinywasm/layout<br/>Dir = layout/"]
    C["se recorre layout/ entero<br/>+ cada dependencia en GOMODCACHE"]

    A --> B --> C

    C --> D["layout/platformd<br/>css.go con RenderCSS()"]
    C --> E["layout/platformd/web<br/>package main + //go:build wasm"]
    C --> F["components/usermenu<br/>(dependencia, importada)"]
    C --> G["components/calendarslider<br/>(dependencia, NO importada)"]
    C --> H["components/calendarslider/web<br/>package main + //go:build wasm"]

    D --> D1["✅ seleccionado"]
    E --> E1["❌ regla 2: package main"]
    F --> F1["✅ seleccionado"]
    G --> G1["❌ fuera de alcance (regla 1)<br/>nadie lo importa → nunca entra al main.go<br/>el error de go.sum NO puede ocurrir"]
    H --> H1["❌ regla 2: package main"]
```

**Hoy** el caso G revienta el `go run` entero → `/style.css` = 0 bytes y la app
sin estilos. **Con el arreglo** calendarslider ni siquiera se considera: no está
en el grafo de compilación, así que el `missing go.sum entry` deja de existir
como clase de fallo. No se "salta un error", se elimina la causa.

---

## Caso B — un solo componente

Arranque en `components/calendarslider`. El `go.mod` está en `components/`,
o sea **por encima** del directorio de arranque.

```mermaid
flowchart TD
    A["DIR_ARRANQUE = components/calendarslider"]
    B["go.mod está en components/<br/>módulo main = github.com/tinywasm/components<br/>Dir = components/  ⚠️ NO calendarslider"]
    C["el walk parte de components/ ENTERO<br/>pero el ALCANCE se calcula desde<br/>DIR_ARRANQUE = calendarslider"]

    A --> B --> C

    C --> D["calendarslider/css.go<br/>RenderCSS()"]
    C --> E["calendarslider/web<br/>package main, en el módulo MAIN"]
    C --> F["selectsearch/web<br/>themetoggle/web<br/>package main, en el módulo MAIN"]
    C --> G["selectsearch/css.go<br/>targetlist/css.go<br/>+ 10 componentes más"]

    D --> D1["✅ seleccionado<br/>es lo que se quiere probar"]
    E --> E1["❌ regla 2: package main<br/>⚠️ aquí NO sirve exentar 'módulo main'"]
    F --> F1["❌ regla 2: package main"]
    G --> G1["❌ fuera de alcance<br/>no están en el grafo de calendarslider<br/>regla 1"]
```

### Por qué la regla no puede depender del módulo

```
$ cd components/calendarslider && go list -m -json
Path: github.com/tinywasm/components
Dir:  /home/cesar/Dev/Project/tinywasm/components    <- raíz del módulo
Main: true
```

En este caso las demos están en el módulo **main**. Una regla del tipo
"saltar `web/` solo en dependencias" dejaría el Caso B sin protección. Por eso
la regla 2 mira **la cláusula `package`**, no el módulo ni el nombre del
directorio.

### El alcance se ancla en DIR_ARRANQUE, no en la raíz del módulo

El walk parte de la raíz del módulo, pero el **filtro** se calcula desde
`DIR_ARRANQUE`. Por eso probar `calendarslider` extrae su grafo y nada más,
aunque el `go.mod` esté dos niveles arriba y el módulo tenga trece componentes.

Medido en `layout/platformd`: servidor 7 paquetes, cliente 8, **unión 9**,
mientras que hoy se importan **13**.

## Resumen de las dos preguntas que decide el flujo

| Pregunta | Caso A | Caso B |
|---|---|---|
| ¿Dónde arranco? | `layout/platformd` | `components/calendarslider` |
| ¿Dónde está el `go.mod`? | `layout/` (un nivel arriba) | `components/` (un nivel arriba) |
| ¿El dir de arranque es la raíz del módulo? | no | no |
| ¿Dónde vive el cliente/demo? | `platformd/web/` | `calendarslider/web/` → propuesta: `example/` |
| ¿Las demos están en el módulo main? | sí (`platformd/web`) | sí (las tres) |
| ¿Qué se recorre? | `layout/` + dependencias | `components/` entero |
| ¿Qué se **extrae** tras el arreglo? | grafo de `platformd` | grafo de `calendarslider` |
| Fallo actual | `/style.css` = 0 bytes | funciona, pero con CSS de 13 componentes |
| Tras el arreglo | 9 paquetes usados, CSS completo | solo el CSS de calendarslider |
