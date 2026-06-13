package service

import "testing"

func TestIsTrivialDoneOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"plain english", "done", true},
		{"english punctuation", " Done. ", true},
		{"russian", "Готово!", true},
		{"russian feminine", "готова…", true},
		{"russian done", "Сделано", true},
		{"chinese", "完成！", true},
		{"japanese", "完了。", true},
		{"not only marker", "done, see PR", false},
		{"not acknowledgement", "好的", false},
		{"real answer", "I fixed the issue", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isTrivialDoneOutput(tt.in); got != tt.want {
				t.Fatalf("isTrivialDoneOutput(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeAgentVisibleOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "plain text unchanged",
			in:   "Done with details",
			want: "Done with details",
		},
		{
			name: "openclaw nested result payload",
			in: `{
				"runId": "run-1",
				"status": "ok",
				"result": {
					"payloads": [
						{"text": "Готово. Posted artifact comment."},
						{"text": "⚠️ 🛠️ multica issue get VID-1 --output json failed"}
					]
				}
			}`,
			want: "Готово. Posted artifact comment.",
		},
		{
			name: "legacy payloads",
			in: `{
				"payloads": [
					{"text": "First"},
					{"text": "Second"}
				],
				"meta": {"durationMs": 10}
			}`,
			want: "First\n\nSecond",
		},
		{
			name: "technical only suppresses",
			in: `{
				"result": {
					"payloads": [
						{"text": "⚠️ 🛠️ tool failed"}
					]
				}
			}`,
			want: "",
		},
		{
			name: "non envelope json unchanged",
			in:   `{"artifact_name":"qc-report.md"}`,
			want: `{"artifact_name":"qc-report.md"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAgentVisibleOutput(tt.in); got != tt.want {
				t.Fatalf("normalizeAgentVisibleOutput() = %q, want %q", got, tt.want)
			}
		})
	}
}
