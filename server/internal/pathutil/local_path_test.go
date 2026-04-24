package pathutil

import "testing"

func TestNormalizeLocalPath(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{
			name: "trim and clean posix",
			in:   "  /tmp/my/../proj  ",
			want: "/tmp/proj",
		},
		{
			name:    "reject empty",
			in:      "   ",
			wantErr: true,
		},
		{
			name:    "reject control characters",
			in:      "/tmp/proj\nname",
			wantErr: true,
		},
		{
			name: "accept windows absolute path",
			in:   `C:\Work\Repo`,
			want: `C:\Work\Repo`,
		},
		{
			name: "accept unc path",
			in:   `\\server\share\repo`,
			want: `\\server\share\repo`,
		},
		{
			name:    "reject relative path",
			in:      "repo/subdir",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeLocalPath(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got path %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("NormalizeLocalPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
