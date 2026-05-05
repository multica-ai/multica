package handler

// CEREBRO-PATCH(setup-jwt-handler): cerebro modification of upstream file

import "os"

// init runs before TestMain — guarantees JWT_SECRET is configured before
// any handler test reaches auth.JWTSecret() (which is gated by sync.Once
// and refuses to boot on an unset/short secret).
func init() {
	if os.Getenv("JWT_SECRET") == "" || len(os.Getenv("JWT_SECRET")) < 32 {
		os.Setenv("JWT_SECRET", "test-jwt-secret-padding-padding-padding-padding")
	}
}
