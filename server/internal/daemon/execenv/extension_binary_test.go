package execenv

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSkillFilesRestoresBase64SupportingAsset(t *testing.T) {
	dir := t.TempDir()
	want := []byte{0x00, 0xff, 0x1a, 0x7f}
	err := writeSkillFiles(dir, []SkillContextForEnv{{
		Name:    "Binary asset skill",
		Content: "---\nname: binary-asset-skill\n---\n",
		Files: []SkillFileContextForEnv{{
			Path:     "assets/icon.bin",
			Content:  base64.StdEncoding.EncodeToString(want),
			Encoding: "base64",
		}},
	}}, nil)
	if err != nil {
		t.Fatalf("writeSkillFiles() error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "binary-asset-skill", "assets", "icon.bin"))
	if err != nil {
		t.Fatalf("read restored asset: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("asset bytes = %v, want %v", got, want)
	}
}
