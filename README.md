# ssr
<img src="docs/img/badges.svg">

SSR asset extractor for TinyWasm: runs components Render methods and collects CSS/JS/HTML/SVG for assetmin.

Module discovery is delegated to [modfind](https://github.com/tinywasm/modfind), ensuring shared and cached module lookups across TinyWasm tools.

## SSR Package Contract

Every package containing SSR files (`css.go`, `js.go`, `svg.go`, `html.go`) must export a function named exactly `SSR` returning a slice of `widget.Widget`s to participate in typed asset extraction:

```go
package mycomponent

import "github.com/tinywasm/widget"

func SSR() []widget.Widget {
	return []widget.Widget{
		&MyWidget{},
	}
}
```

If a package with SSR source files misses this function, it will fail at compile time during the extraction phase.

## Documentation

- [Documentation Guidelines](docs/DOCUMENTATION.md)
