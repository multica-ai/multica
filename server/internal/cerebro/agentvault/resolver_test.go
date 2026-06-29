package agentvault

import "testing"

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
