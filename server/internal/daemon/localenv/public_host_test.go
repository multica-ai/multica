package localenv

import (
	"strings"
	"testing"
)

func TestPublicHostURLs(t *testing.T) {
	for _, tc := range []struct{ host, want string }{
		{"", "http://localhost:41000"}, {"lan.example", "http://lan.example:41000"},
		{"http://lan.example/", "http://localhost:41000"}, {"[::1]", "http://[::1]:41000"},
		{"2001:db8::cafe", "http://[2001:db8::cafe]:41000"}, {"192.168.1.10", "http://192.168.1.10:41000"},
		{"bad/path", "http://localhost:41000"}, {"bad_name", "http://localhost:41000"},
		{"lab.example:80", "http://localhost:41000"}, {"1.2.3.999", "http://localhost:41000"},
		{"[lab.example]", "http://localhost:41000"}, {"-bad.example", "http://localhost:41000"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			t.Setenv("MULTICA_ENV_PUBLIC_HOST", tc.host)
			if got := detectedServerURL(41000); got != tc.want {
				t.Fatalf("detected=%q want %q", got, tc.want)
			}
			for _, raw := range []string{"http://localhost:$MULTICA_PORT", "http://$MULTICA_HOST:${MULTICA_PORT}", "http://${MULTICA_HOST}:$MULTICA_PORT"} {
				if got := expandEnvURL(raw, 41000, publicHostFromEnv()); got != tc.want {
					t.Fatalf("expanded=%q want %q", got, tc.want)
				}
			}
		})
	}
	if got := expandEnvURL("https://custom.example/docs?q=1", 80, "lan.example"); got != "https://custom.example/docs?q=1" {
		t.Fatal(got)
	}
}

func TestPublicHostReadyIsolation(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		raw := "[environments.web]\nstart='true'\nurl='http://$MULTICA_HOST:$MULTICA_PORT/docs'\n"
		if explicit {
			raw += "ready='http://$MULTICA_HOST:$MULTICA_PORT/health'\n"
		}
		cfg, err := ParseConfig([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		def := cfg.Environments["web"]
		want := "http://127.0.0.1:41000/docs"
		if explicit {
			want = "http://lan.example:41000/health"
		}
		if got := expandReadyURL(def, 41000, "lan.example"); got != want {
			t.Fatalf("ready=%q want %q", got, want)
		}
	}
	t.Setenv("MULTICA_ENV_PUBLIC_HOST", "lan.example")
	t.Setenv("MULTICA_TOKEN", "secret")
	env := strings.Join(childEnv("", 41000, "web"), "\n")
	if !strings.Contains(env, "MULTICA_HOST=lan.example") || strings.Contains(env, "MULTICA_TOKEN=") {
		t.Fatal("child environment host/credential boundary")
	}
}

func TestPublicHostDefaultReadyCompatibility(t *testing.T) {
	t.Setenv("MULTICA_ENV_PUBLIC_HOST", "")
	for _, setting := range []string{"", "url='http://localhost:$MULTICA_PORT/app'", "ready='http://localhost:${MULTICA_PORT}/health'", "url=''"} {
		cfg, err := ParseConfig([]byte("[environments.web]\nstart='true'\n" + setting))
		if err != nil {
			t.Fatal(err)
		}
		def := cfg.Environments["web"]
		if got, want := expandReadyURL(def, 41000, publicHostFromEnv()), expandPort(def.Ready, 41000); got != want {
			t.Errorf("%s: ready=%q want %q", setting, got, want)
		}
	}
	def := Definition{URL: "http://$MULTICA_HOST:$MULTICA_PORT", Ready: "http://$MULTICA_HOST:$MULTICA_PORT"}
	if got := expandReadyURL(def, 41000, publicHostFromEnv()); got != "http://localhost:41000" {
		t.Fatalf("default host placeholder: %q", got)
	}
}
