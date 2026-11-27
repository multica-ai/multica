package handler

import (
	"archive/zip"
	"bytes"
	"testing"
)

func TestParseSkillArchive_SkipsWindowsDirectoryMarkers(t *testing.T) {
	for _, prefix := range []string{"", `review-helper\`} {
		t.Run(prefix, func(t *testing.T) {
			var buf bytes.Buffer
			zw := zip.NewWriter(&buf)
			for name, content := range map[string]string{
				prefix + "SKILL.md":                   "---\nname: review-helper\n---\n# Review",
				prefix + `references\`:                "",
				prefix + `references\guides\setup.md`: "setup guide",
				prefix + `assets\`:                    "",
				`SKILL.md\`:                           "",
			} {
				w, err := zw.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := w.Write([]byte(content)); err != nil {
					t.Fatal(err)
				}
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range zr.File {
				if f.FileInfo().IsDir() {
					t.Fatalf("fixture unexpectedly has directory attributes: %q", f.Name)
				}
			}
			imported, err := parseSkillArchive(buf.Bytes(), "review-helper.zip")
			if err != nil {
				t.Fatal(err)
			}
			if imported.content != "---\nname: review-helper\n---\n# Review" {
				t.Fatalf("wrong primary content: %q", imported.content)
			}
			if len(imported.files) != 1 || imported.files[0].path != "references/guides/setup.md" || imported.files[0].content != "setup guide" {
				t.Fatalf("expected only the nested supporting file, got %#v", imported.files)
			}
		})
	}
}
