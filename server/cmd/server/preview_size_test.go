package main

import (
	"testing"

	"github.com/multica-ai/multica/server/internal/handler"
)

func TestConfiguredMaxPreviewSize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int64
	}{
		{name: "default", raw: "", want: handler.DefaultMaxPreviewSizeBytes},
		{name: "custom", raw: "8MiB", want: 8 << 20},
		{name: "maximum", raw: "16MiB", want: handler.MaxConfigurablePreviewSizeBytes},
		{name: "invalid falls back", raw: "unbounded", want: handler.DefaultMaxPreviewSizeBytes},
		{name: "extreme falls back", raw: "9223372036854775807", want: handler.DefaultMaxPreviewSizeBytes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := configuredMaxPreviewSize(tt.raw); got != tt.want {
				t.Fatalf("configuredMaxPreviewSize(%q) = %d, want %d", tt.raw, got, tt.want)
			}
		})
	}
}
