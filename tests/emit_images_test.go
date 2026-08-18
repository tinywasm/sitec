//go:build !wasm

package sitec_test

import (
	"testing"

	imgmin "github.com/tinywasm/image/min"
	"github.com/tinywasm/router/mock"
	"github.com/tinywasm/sitec"
)

type stubImageProcessor struct {
	artifacts []imgmin.Artifact
}

func (s *stubImageProcessor) UnobservedFiles() []string {
	return nil
}

func (s *stubImageProcessor) Artifacts() []imgmin.Artifact {
	return s.artifacts
}

func TestPublishImagesHaceServibleUnaImagen(t *testing.T) {
	setup := newTestSetup(t)
	defer setup.cleanup()

	am := sitec.NewAssetMin(setup.ac)
	am.SetFS(sitec.NewMemFS())

	ip := &stubImageProcessor{
		artifacts: []imgmin.Artifact{
			{
				Path:      "img/foto.jpg",
				Mediatype: "image/jpeg",
				Content:   []byte("fake-jpeg-bytes"),
			},
		},
	}
	am.SetImageProcessor(ip)

	if err := am.PublishImages(); err != nil {
		t.Fatalf("PublishImages failed: %v", err)
	}

	content, mediatype, ok := am.Read("img/foto.jpg")
	if !ok {
		t.Fatalf("am.Read failed for img/foto.jpg")
	}
	if string(content) != "fake-jpeg-bytes" {
		t.Errorf("expected 'fake-jpeg-bytes', got %q", string(content))
	}
	if mediatype != "image/jpeg" {
		t.Errorf("expected 'image/jpeg', got %q", mediatype)
	}

	r := newTestRouter(am)
	ctx := &mock.Context{
		InPath:   "/img/foto.jpg",
		InMethod: "GET",
	}
	r.Invoke("GET", "/img/foto.jpg", ctx)

	if string(ctx.ResponseBody()) != "fake-jpeg-bytes" {
		t.Errorf("expected HTTP response body 'fake-jpeg-bytes', got %q", string(ctx.ResponseBody()))
	}
	if ctx.GetHeader("Content-Type") != "image/jpeg" {
		t.Errorf("expected Content-Type 'image/jpeg', got %q", ctx.GetHeader("Content-Type"))
	}
}
