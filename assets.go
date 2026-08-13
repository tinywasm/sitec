package sitec

import (
	"github.com/tinywasm/font"
	"github.com/tinywasm/js"
	"github.com/tinywasm/svg/sprite"
)

// Assets is what compiling one module yields: the raw output its producers
// emitted, before minification and before anything is written to disk.
//
// This type is the contract between the compiler and whoever consumes its
// output. It lives here, with the producer. It used to live in
// github.com/tinywasm/assetmin — the consumer — which forced the producer to
// import the minifier, and with it an HTTP router and a terminal UI, just to
// name its own result.
type Assets struct {
	ModuleName  string
	RootCSS     string
	CSS         string
	JS          []*js.Script
	HTML        string
	Icons       *sprite.Sprite
	Fonts       font.Declaration // family declared by the module; zero value = none
	IsRoot      bool
	IsFramework bool
}
