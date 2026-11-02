//go:build agentintegration

package agent

import (
	"context"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAntigravityNativeImageLive(t *testing.T) {
	requireRealAgentSmoke(t)
	path, err := exec.LookPath("agy")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	command := NewCommand(path, nil)
	if err := ProbeAntigravityImages(ctx, command, home); err != nil {
		t.Fatal(err)
	}
	var inputs []string
	for i, prompt := range []string{
		"Generate a solid blue circle centered on a plain white background. No text.",
		"Edit the reference image: change the blue circle to red, keeping the white background, shape and position.",
	} {
		result, err := GenerateAntigravityImage(ctx, command, home, t.TempDir(), prompt, inputs)
		if err != nil || result.RateLimited {
			t.Fatalf("native image error=%v rate_limited=%v", err, result.RateLimited)
		}
		f, err := os.Open(result.Path)
		if err != nil {
			t.Fatal(err)
		}
		cfg, format, decodeErr := image.DecodeConfig(f)
		f.Close()
		if decodeErr != nil || cfg.Width < 1 || cfg.Height < 1 {
			t.Fatalf("invalid native image: %v", decodeErr)
		}
		t.Logf("reference_images=%d format=%s dimensions=%dx%d", len(inputs), format, cfg.Width, cfg.Height)
		if outDir := os.Getenv("MULTICA_MEDIA_SMOKE_OUTPUT_DIR"); outDir != "" {
			if err := os.MkdirAll(outDir, 0700); err != nil {
				t.Fatal(err)
			}
			name := []string{"generated", "edited"}[i] + filepath.Ext(result.Path)
			source, err := os.Open(result.Path)
			if err != nil {
				t.Fatal(err)
			}
			out, err := os.OpenFile(filepath.Join(outDir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if err != nil {
				source.Close()
				t.Fatal(err)
			}
			_, copyErr := io.Copy(out, source)
			closeErr := out.Close()
			source.Close()
			if copyErr != nil || closeErr != nil {
				t.Fatalf("save smoke image: %v %v", copyErr, closeErr)
			}
		}
		inputs = []string{result.Path}
	}
}
