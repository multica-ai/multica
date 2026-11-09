package execenv

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestHermesTurnEnvironmentReplacesNamesOnReuse(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.yaml")
	mustWrite(t, path, "terminal:\n  timeout: 42\n  client_env_turn_passthrough: &names [STALE_KEY]\ncopy: *names\n")
	for _, names := range [][]string{{"SERVICE_KEY", "EMPTY_KEY", "SERVICE_KEY"}, {"NEXT_KEY"}} {
		if err := ConfigureHermesTurnEnvironment(home, names); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Terminal struct {
				Timeout int
				Names   []string `yaml:"client_env_turn_passthrough"`
			}
			Copy []string
		}
		if err := yaml.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		want := []string{"EMPTY_KEY", "SERVICE_KEY"}
		if len(names) == 1 {
			want = names
		}
		if !reflect.DeepEqual(got.Terminal.Names, want) || got.Terminal.Timeout != 42 || !reflect.DeepEqual(got.Copy, []string{"STALE_KEY"}) {
			t.Fatalf("unexpected config: %+v", got)
		}
	}
}
