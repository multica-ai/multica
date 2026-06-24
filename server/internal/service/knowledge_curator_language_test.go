package service

import "testing"

func TestInferCuratorOutputLanguageFromEvidence(t *testing.T) {
	tests := []struct {
		name string
		in   curatorLanguageEvidence
		want string
	}{
		{
			name: "Chinese issue with English technical logs",
			in: curatorLanguageEvidence{
				Title:       "本地消息同步失败",
				Description: "用户点击沉淀经验后返回 502。\nError: context deadline exceeded\nserver/internal/service/local_run_reporter.go:42\ncurl -i http://127.0.0.1:3001/health",
				Comments:    []string{"重试后仍然失败，需要保留日志原文。"},
				Tasks:       []string{"failed\nError: context deadline exceeded"},
			},
			want: curatorOutputLanguageChinese,
		},
		{
			name: "English issue",
			in: curatorLanguageEvidence{
				Title:       "Local runtime drops sync messages",
				Description: "The daemon should persist retry payloads when the server returns a timeout.",
				Comments:    []string{"This should become a reusable operations playbook."},
			},
			want: curatorOutputLanguageEnglish,
		},
		{
			name: "Ambiguous technical evidence",
			in: curatorLanguageEvidence{
				Title:       "502",
				Description: "panic: nil pointer\nserver/internal/service/task.go:10\npnpm test",
			},
			want: curatorOutputLanguageFallback,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferCuratorOutputLanguageFromEvidence(tt.in)
			if got != tt.want {
				t.Fatalf("inferCuratorOutputLanguageFromEvidence() = %q, want %q", got, tt.want)
			}
		})
	}
}
