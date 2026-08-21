//go:build !wasm

package sitec_test

import (
	"testing"

	"github.com/tinywasm/router/mock"
	"github.com/tinywasm/sitec"
	"github.com/tinywasm/sitec/serve"
	"os"
	"path/filepath"
)

type testSetup struct {
	t         *testing.T
	outputDir string
	ac        *sitec.Config
}

func newTestSetup(t *testing.T) *testSetup {
	outputDir := t.TempDir()

	ac := &sitec.Config{
		OutputDir: outputDir,
	}

	return &testSetup{
		t:         t,
		outputDir: outputDir,
		ac:        ac,
	}
}

func (s *testSetup) cleanup() {
	// t.TempDir handles cleanup
}

func newTestRouter(am *sitec.AssetMin) *mock.Router {
	r := &mock.Router{}
	serve.RegisterRoutes(r, am)
	return r
}

func (s *testSetup) createTempFile(name, content string) string {
	path := filepath.Join(s.outputDir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		s.t.Fatalf("Failed to create temp file %s: %v", name, err)
	}
	return path
}
