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
