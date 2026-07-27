package ssr

import (
	"github.com/tinywasm/js"
	"github.com/tinywasm/svg/sprite"
	"github.com/tinywasm/widget"
	"github.com/tinywasm/widget/style"
)

// Styler represents components that provide custom styles.
type Styler interface {
	Style() *style.Sheet
}

// HTMLProvider represents components that provide HTML.
type HTMLProvider interface {
	HTML() string
}

// JSProvider represents components that provide scripts.
type JSProvider interface {
	JS() []*js.Script
}

// IconProvider represents components that provide SVG icons.
type IconProvider interface {
	Icons() *sprite.Sprite
}

// ScriptOutput matches the JSON output structure for scripts.
type ScriptOutput struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// Bundle represents the aggregated SSR assets.
type Bundle struct {
	Render  string         `json:"render"`
	HTML    string         `json:"html"`
	Scripts []ScriptOutput `json:"scripts"`
	Icons   *sprite.Sprite `json:"icons"`
}

// NewBundle creates a new, empty asset Bundle.
func NewBundle() *Bundle {
	return &Bundle{
		Icons: sprite.NewSprite(),
	}
}

// AddSheet appends the rendered CSS from a Style sheet to the Render section.
func (b *Bundle) AddSheet(sheet *style.Sheet) {
	if sheet == nil {
		return
	}
	b.Render += sheet.Stylesheet().String()
}

// AddIcons merges icons into the Bundle's sprite sheet.
func (b *Bundle) AddIcons(icons *sprite.Sprite) {
	if icons == nil {
		return
	}
	b.Icons.Merge(icons)
}

// AddHTML appends HTML content.
func (b *Bundle) AddHTML(html string) {
	b.HTML += html
}

// AddJS adds the compiled scripts to the Bundle.
func (b *Bundle) AddJS(scripts []*js.Script) {
	for _, s := range scripts {
		if s == nil {
			continue
		}
		b.Scripts = append(b.Scripts, ScriptOutput{
			Name:    s.Name,
			Content: s.Content,
		})
	}
}

// Collect gathers a set of widgets and aggregates their SSR assets based on capabilities.
func Collect(parts ...widget.Widget) *Bundle {
	b := NewBundle()
	for _, p := range parts {
		if s, ok := p.(Styler); ok {
			b.AddSheet(s.Style())
		}
		if i, ok := p.(IconProvider); ok {
			b.AddIcons(i.Icons())
		}
		if h, ok := p.(HTMLProvider); ok {
			b.AddHTML(h.HTML())
		}
		if j, ok := p.(JSProvider); ok {
			b.AddJS(j.JS())
		}
	}
	return b
}
