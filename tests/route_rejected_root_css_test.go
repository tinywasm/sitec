//go:build !wasm

package sitec_test

import (
	"strings"
	"testing"

	"github.com/tinywasm/sitec"
)

// TestRouteExtractedAssets_RejectsUnauthorizedRootCSS locks in that a module
// which declares RootCSS() without qualifying as the app's own root or the
// tinywasm/css framework fails the build loudly. Before this fix,
// routeAssets only logged a warning and moved on — a CI/CD `sitec build`
// checks the exit code, not stderr, so a misidentified root silently shipped
// the framework's raw defaults instead of failing the pipeline.
func TestRouteExtractedAssets_RejectsUnauthorizedRootCSS(t *testing.T) {
	am := sitec.NewAssetMin(&sitec.Config{})

	err := am.RouteExtractedAssets([]*sitec.Assets{
		{
			ModuleName:  "example.com/some/dependency",
			RootCSS:     ":root{--should-not-apply:1;}",
			IsRoot:      false,
			IsFramework: false,
		},
	})

	if err == nil {
		t.Fatal("RouteExtractedAssets returned nil error for a module that is neither root nor framework " +
			"but declares RootCSS() — this must fail the build, not silently drop the theme")
	}
	if !strings.Contains(err.Error(), "example.com/some/dependency") {
		t.Errorf("error should name the offending module so a CI log points at the fix, got: %v", err)
	}
}
