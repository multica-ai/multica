package localmode

import "testing"

func TestConfigEnabledWhenProductModeIsLocal(t *testing.T) {
	t.Setenv("MULTICA_PRODUCT_MODE", "local")

	if !FromEnv().Enabled() {
		t.Fatal("expected local mode to be enabled")
	}
}

func TestConfigDisabledByDefault(t *testing.T) {
	t.Setenv("MULTICA_PRODUCT_MODE", "")

	if FromEnv().Enabled() {
		t.Fatal("expected local mode to be disabled by default")
	}
}

func TestConfigDisabledForOtherProductModes(t *testing.T) {
	t.Setenv("MULTICA_PRODUCT_MODE", "cloud")

	if FromEnv().Enabled() {
		t.Fatal("expected local mode to be disabled for non-local product mode")
	}
}
