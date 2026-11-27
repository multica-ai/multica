package redact

import (
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"
)

// envFile is what `cat .env` prints for a typical service.
func envFile() string {
	return "APP_ENV=production\nOPENAI_API_KEY=" + asm("sk-", "proj", "AbCdEfGhIjKlMnOpQrStUvWx") +
		"\nDATABASE_URL=" + asm("postgres://app:", "hunter2hunter2", "@db.internal:5432/app") + "\n"
}

func wrap(s string, width int) string {
	var b strings.Builder
	for len(s) > width {
		b.WriteString(s[:width])
		b.WriteString("\n")
		s = s[width:]
	}
	b.WriteString(s)
	return b.String()
}

func assertBase64Redacted(t *testing.T, input, encoded string) {
	t.Helper()
	got := Text(input)
	if !strings.Contains(got, base64Placeholder) {
		t.Fatalf("encoded secret not redacted: %q", got)
	}
	for _, line := range strings.Split(encoded, "\n") {
		if len(line) >= 16 && strings.Contains(got, line) {
			t.Fatalf("encoded secret survived redaction: %q", got)
		}
	}
}

func TestRedactBase64EncodedSecrets(t *testing.T) {
	t.Parallel()
	std := base64.StdEncoding.EncodeToString([]byte(envFile()))
	url := base64.RawURLEncoding.EncodeToString([]byte(envFile()))
	twice := base64.StdEncoding.EncodeToString([]byte(std))

	cases := map[string]struct{ input, encoded string }{
		"one line":             {"$ cat .env | base64\n" + std + "\n$ ", std},
		"wrapped at 76":        {wrap(std, 76) + "\n", wrap(std, 76)},
		"cut by truncation":    {std[:len(std)-7] + "…[truncated]", std[:len(std)-7]},
		"url-safe, glued":      {"payload_" + url, url},
		"inside json":          {`{"data":"` + std + `"}`, std},
		"encoded twice":        {twice, twice},
		"after a secret label": {"value: " + std, std},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertBase64Redacted(t, tc.input, tc.encoded)
		})
	}
}

// encodeAs returns the base64 of s grown by newlines until the encoding ends
// with padding or, when padded is false, with none.
func encodeAs(t *testing.T, s string, padded bool) string {
	t.Helper()
	for (len(s)%3 != 0) != padded {
		s += "\n"
	}
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	if strings.HasSuffix(enc, "=") != padded {
		t.Fatalf("encodeAs(padded=%v) = %q", padded, enc)
	}
	return enc
}

// Tool output rarely ends at the encoded secret: another encoded value, a
// status word or a path follows on the next line, or a suffix on the same one.
func TestRedactBase64SecretsNextToOtherOutput(t *testing.T) {
	t.Parallel()
	first := encodeAs(t, envFile(), true)
	second := encodeAs(t, "GITHUB_TOKEN="+asm("ghp_", "AbCdEfGhIjKlMnOpQrSt", "UvWxYz0123456789")+"\n", true)
	unpadded := encodeAs(t, envFile(), false)
	path := "server/internal/handler/task_transcript_purge_with_a_long_descriptive_name.go"

	cases := map[string]struct {
		input   string
		encoded []string
	}{
		"two padded lines":            {first + "\n" + second + "\n", []string{first, second}},
		"padded, then a word":         {first + "\ndone_running\n", []string{first}},
		"unpadded, then a path":       {unpadded + "\nserver/handler\n", []string{unpadded}},
		"unpadded, glued suffix":      {unpadded + "_tail", []string{unpadded}},
		"wrapped, then a word":        {wrap(unpadded, 76) + "\nexit_code_zero\n", []string{wrap(unpadded, 76)}},
		"after a long path line":      {path + "\n" + first + "\n", []string{first}},
		"glued to a long prefix word": {strings.Repeat("prefix_", 12) + unpadded, []string{unpadded}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, encoded := range tc.encoded {
				assertBase64Redacted(t, tc.input, encoded)
			}
		})
	}
}

func TestRedactBase64LeavesHarmlessRunsAlone(t *testing.T) {
	t.Parallel()
	binary := make([]byte, 4096)
	rand.New(rand.NewSource(1)).Read(binary)
	png := append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), binary...)

	for name, input := range map[string]string{
		"encoded prose":   base64.StdEncoding.EncodeToString([]byte("The quick brown fox jumps over the lazy dog, twice over.")),
		"encoded binary":  base64.StdEncoding.EncodeToString(binary),
		"encoded image":   "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"git sha":         "commit 467b6f4b49e0c1d2a3b4c5d6e7f8091a2b3c4d5e",
		"identifiers":     "TestRedactBase64LeavesHarmlessRunsAlone and server/internal/handler/task_transcript_purge.go",
		"sha256 checksum": "sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := Text(input); got != input {
				t.Fatalf("harmless text changed:\n got: %q\nwant: %q", got, input)
			}
		})
	}
}

func TestRedactBase64HugeRunIsNotDecoded(t *testing.T) {
	t.Parallel()
	secret := strings.Repeat(envFile(), maxBase64Run/len(envFile())+1)
	input := base64.StdEncoding.EncodeToString([]byte(secret))
	if got := Text(input); got != input {
		t.Fatal("a run past maxBase64Run was decoded")
	}
}

// BenchmarkTextToolOutput measures a full 8 KiB tool-output preview, the
// largest string ingest redacts per message.
func BenchmarkTextToolOutput(b *testing.B) {
	binary := make([]byte, 3000)
	rand.New(rand.NewSource(1)).Read(binary)
	var sb strings.Builder
	for sb.Len() < 8<<10 {
		sb.WriteString("commit 467b6f4b49e0c1d2a3b4c5d6e7f8091a2b3c4d5e\nserver/internal/handler/daemon.go:6066\n")
		sb.WriteString(wrap(base64.StdEncoding.EncodeToString(binary), 76))
		sb.WriteString("\n")
	}
	output := sb.String()[:8<<10]
	b.SetBytes(int64(len(output)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Text(output)
	}
}
