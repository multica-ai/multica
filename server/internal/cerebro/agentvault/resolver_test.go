package agentvault

import "testing"

func TestBuildSpawnEnvEmptyProxyYieldsNil(t *testing.T) {
	if env := BuildSpawnEnv("", "/tmp/ca.pem"); env != nil {
		t.Fatalf("expected nil env for empty proxy, got %v", env)
	}
}

func TestBuildSpawnEnvSetsProxyAndCA(t *testing.T) {
	env := BuildSpawnEnv("http://tok@multica.internal:8080", "/tmp/ca.pem")
	if env[envHTTPSProxy] != "http://tok@multica.internal:8080" {
		t.Fatalf("HTTPS_PROXY not set: %v", env)
	}
	if env[envHTTPProxy] != env[envHTTPSProxy] {
		t.Fatalf("HTTP_PROXY should equal HTTPS_PROXY")
	}
	for _, k := range []string{envSSLCertFile, envNodeExtraCA, envRequestsCA, envCurlCABundle} {
		if env[k] != "/tmp/ca.pem" {
			t.Fatalf("CA var %s not set: %v", k, env)
		}
	}
}

func TestBuildSpawnEnvNoCAOmitsCAVars(t *testing.T) {
	env := BuildSpawnEnv("http://tok@multica.internal:8080", "")
	if _, ok := env[envSSLCertFile]; ok {
		t.Fatalf("CA vars should be absent when caPath empty: %v", env)
	}
}

func TestAllowedVaultsDistinctInOrderSkipsBlank(t *testing.T) {
	got := AllowedVaults([]Access{
		{Vault: "multica"}, {Vault: " "}, {Vault: "cloudflare"}, {Vault: "multica"},
	})
	want := []string{"multica", "cloudflare"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
