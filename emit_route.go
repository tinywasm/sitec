package sitec

// Movido desde assetmin/ssr_loader.go — la MITAD de compilacion.
// Decide que modulo aporta el RootCSS, los slots de cascada y la copia de
// fuentes: politica de como se ensambla la salida, no de cuando se reintenta.

func (c *AssetMin) routeAssets(a *Assets, isRoot, isFramework bool) error {
	if isRoot {
		c.fromRoot = nil
	} else if isFramework {
		c.fromCss = nil
	}

	if a.RootCSS != "" {
		switch {
		case isRoot:
			c.fromRoot = &rootCandidate{name: a.ModuleName, css: a.RootCSS}
		case isFramework:
			c.fromCss = &rootCandidate{name: a.ModuleName, css: a.RootCSS}
		default:
			c.Logger("warning: module", a.ModuleName, "declares RootCSS() but only the root project or", cssModulePath, "may; ignoring")
		}
	}

	if a.Fonts.Family() != "" {
		if isRoot {
			if err := c.copyDeclaredFonts(a.Fonts); err != nil {
				return err
			}
			c.setFonts(a.Fonts)
		} else {
			c.Logger("warning: module", a.ModuleName, "declares Fonts() but only the root project may; ignoring")
		}
	} else if isRoot {
		c.setFonts(a.Fonts) // clear if root no longer declares fonts
	}

	slot := "middle"
	if isRoot {
		// El slot "close" para el raíz es una decisión de cascada CSS/JS
		// (su CSS gana sobre el de los módulos). El HTML IGNORA el slot:
		// updateSSRModuleInSlot lo rutea siempre a "middle", porque
		// contentClose arranca con `</div>` — HTML en "close" quedaría fuera
		// de #app (para el raíz, incluso después de </html>).
		slot = "close"
	}
	// RootCSS deliberately NOT passed here — it has its own slot resolution above.
	c.updateSSRModuleInSlot(a.ModuleName, a.CSS, a.JS, a.HTML, a.Icons, slot)
	return nil
}

func (c *AssetMin) resolveAndApplyRootCSS() {
	var entries []*ContentFile
	if c.fromRoot != nil {
		entries = append(entries, &ContentFile{Path: c.fromRoot.name, Content: []byte(c.fromRoot.css)})
	} else if c.fromCss != nil {
		entries = append(entries, &ContentFile{Path: c.fromCss.name, Content: []byte(c.fromCss.css)})
	}

	c.mainStyleCssHandler.mu.Lock()
	c.mainStyleCssHandler.contentOpen = entries
	c.mainStyleCssHandler.cacheValid = false
	c.mainStyleCssHandler.mu.Unlock()
}

func isRootDir(dir, rootDir string) bool {
	if rootDir == "" {
		return false
	}
	absDir, _ := filepath.Abs(dir)
	absRoot, _ := filepath.Abs(rootDir)
	return absDir == absRoot
}
