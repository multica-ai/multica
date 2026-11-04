package handler

import "testing"

// The test-only routes are off unless the flag is 1, and production never has them.
func TestTestRoutesNeedTheFlagAndNeverRunInProduction(t *testing.T) {
	for _, tc := range []struct {
		name, flag, appEnv string
		want               bool
	}{
		{"flag unset", "", "", false},
		{"flag 0", "0", "", false},
		{"flag on", "1", "", true},
		{"flag on in development", "1", "development", true},
		{"flag on in production", "1", "production", false},
		{"flag on in production, any case", "1", " Production ", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(testRoutesEnv, tc.flag)
			t.Setenv("APP_ENV", tc.appEnv)
			if got := TestRoutesEnabled(); got != tc.want {
				t.Fatalf("TestRoutesEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
