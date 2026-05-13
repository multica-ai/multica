package storage

import "testing"

func TestOSSStorageKeyFromURL(t *testing.T) {
	store := &OSSStorage{
		bucket:        "multica-test",
		region:        "cn-hangzhou",
		endpoint:      "https://oss-cn-hangzhou.aliyuncs.com",
		publicBaseURL: "https://cdn.example.com",
	}

	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{
			name:   "custom public base",
			rawURL: "https://cdn.example.com/workspaces/ws-1/file.png",
			want:   "workspaces/ws-1/file.png",
		},
		{
			name:   "default virtual hosted endpoint",
			rawURL: "https://multica-test.oss-cn-hangzhou.aliyuncs.com/workspaces/ws-1/file.png",
			want:   "workspaces/ws-1/file.png",
		},
		{
			name:   "configured path style endpoint",
			rawURL: "https://oss-cn-hangzhou.aliyuncs.com/multica-test/workspaces/ws-1/file.png",
			want:   "workspaces/ws-1/file.png",
		},
		{
			name:   "configured virtual hosted endpoint",
			rawURL: "https://multica-test.oss-cn-hangzhou.aliyuncs.com/workspaces/ws-1/a%20b.png",
			want:   "workspaces/ws-1/a b.png",
		},
		{
			name:   "fallback",
			rawURL: "https://example.com/files/file.png",
			want:   "file.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := store.KeyFromURL(tt.rawURL); got != tt.want {
				t.Fatalf("KeyFromURL(%q) = %q, want %q", tt.rawURL, got, tt.want)
			}
		})
	}
}

func TestOSSStorageCdnDomain(t *testing.T) {
	store := &OSSStorage{
		bucket:        "multica-test",
		region:        "cn-hangzhou",
		publicBaseURL: "https://cdn.example.com",
	}
	if got := store.CdnDomain(); got != "cdn.example.com" {
		t.Fatalf("CdnDomain with public base = %q, want cdn.example.com", got)
	}

	store.publicBaseURL = ""
	if got := store.CdnDomain(); got != "multica-test.oss-cn-hangzhou.aliyuncs.com" {
		t.Fatalf("CdnDomain without public base = %q, want multica-test.oss-cn-hangzhou.aliyuncs.com", got)
	}
}
