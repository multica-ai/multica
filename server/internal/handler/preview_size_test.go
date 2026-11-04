package handler

import "testing"

func TestMaxPreviewSizeBytes(t *testing.T) {
	for _, size := range []int64{0, -1, 8 << 20, 16 << 20, (16 << 20) + 1, 1<<63 - 1} {
		h := &Handler{cfg: Config{MaxPreviewSizeBytes: size}}
		want := size
		if size <= 0 || size > 16<<20 {
			want = 2 << 20
		}
		if got := h.maxPreviewSizeBytes(); got != want {
			t.Fatalf("configured %d: got %d, want %d", size, got, want)
		}
	}
}
