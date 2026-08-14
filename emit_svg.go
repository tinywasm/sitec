package sitec

import (
	"sort"

	"github.com/tinywasm/fmt"
	"github.com/tinywasm/svg/sprite"
)

func NewSvgHandler(ac *Config, filename string) *asset {
	return newAssetFile(filename, "image/svg+xml", ac, nil)
}

func NewFaviconSvgHandler(ac *Config, filename string) *asset {
	return newAssetFile(filename, "image/svg+xml", ac, nil)
}

// renderSpriteNoLock merges every module's sprite into one. The deduplication
// policy (first occurrence of an ID wins) moved to svg/sprite.MergeAll: it is a
// pure function of sprites and used to be reimplemented here, diverging from
// sprite.Merge. This function now owns only the ORDER — sorted by module name so
// the result is stable across scans.
func (c *AssetMin) renderSpriteNoLock() string {
	var keys []string
	for k := range c.moduleSprites {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ordered := make([]*sprite.Sprite, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, c.moduleSprites[k])
	}

	// sprite.String() now emits EmptyWrapper on its own; no post-patching here.
	return spriteMergeAll(ordered...).String()
}

func (c *AssetMin) renderSprite() string {
	c.spriteMu.RLock()
	defer c.spriteMu.RUnlock()
	return c.renderSpriteNoLock()
}

func (c *AssetMin) setModuleSprite(name string, icons *sprite.Sprite) {
	c.spriteMu.Lock()
	defer c.spriteMu.Unlock()
	if icons == nil {
		delete(c.moduleSprites, name)
	} else {
		if c.moduleSprites == nil {
			c.moduleSprites = make(map[string]*sprite.Sprite)
		}
		c.moduleSprites[name] = icons
	}
	c.spriteSvgHandler.InvalidateCache()
}

// addIcon adds an icon body with its explicit viewBox (the InjectSpriteIcon path).
// viewBox is required: a symbol rendered in a box it was not drawn for is clipped
// or misaligned, and no default can recover the source coordinate system.
func (c *AssetMin) addIcon(id, content, viewBox string) error {
	c.spriteMu.Lock()
	defer c.spriteMu.Unlock()

	if err := c.checkIconID(id); err != nil {
		return err
	}
	if viewBox == "" {
		return fmt.Err("icon requires a viewBox:", id)
	}

	s, ok := c.moduleSprites["_manual"]
	if !ok {
		s = sprite.NewSprite()
		if c.moduleSprites == nil {
			c.moduleSprites = make(map[string]*sprite.Sprite)
		}
		c.moduleSprites["_manual"] = s
	}

	s.AddRaw(id, content, viewBox)
	c.spriteSvgHandler.InvalidateCache()
	return nil
}

// addIconFile adds a whole .svg file as an icon. Reading the file's viewBox and
// stripping its root element is sprite's job — assetmin does not parse SVG.
func (c *AssetMin) addIconFile(id, content string) error {
	c.spriteMu.Lock()
	defer c.spriteMu.Unlock()

	if err := c.checkIconID(id); err != nil {
		return err
	}

	s, ok := c.moduleSprites["_manual"]
	if !ok {
		s = sprite.NewSprite()
		if c.moduleSprites == nil {
			c.moduleSprites = make(map[string]*sprite.Sprite)
		}
		c.moduleSprites["_manual"] = s
	}

	if err := s.AddFile(id, content); err != nil {
		return err
	}
	c.spriteSvgHandler.InvalidateCache()
	return nil
}

func (c *AssetMin) checkIconID(id string) error {
	for _, s := range c.moduleSprites {
		if spriteHas(s, id) {
			return fmt.Err("icon ID already registered:", id)
		}
	}
	return nil
}

func spriteHas(s *sprite.Sprite, id string) bool {
	if s == nil {
		return false
	}
	for _, def := range s.Icons() {
		if def.Icon.ID() == id {
			return true
		}
	}
	return false
}

func spriteMergeAll(sprites ...*sprite.Sprite) *sprite.Sprite {
	res := sprite.NewSprite()
	seen := make(map[string]bool)
	for _, s := range sprites {
		if s == nil {
			continue
		}
		for _, def := range s.Icons() {
			id := def.Icon.ID()
			if !seen[id] {
				seen[id] = true
				res.AddRaw(id, def.Body, def.ViewBox)
			}
		}
	}
	return res
}
