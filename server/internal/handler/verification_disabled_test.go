//go:build !verificationdev

package handler

import (
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/testutil"
)

func TestVerificationProductionRoutesStayAbsent(t *testing.T) {
	t.Setenv("MULTICA_VERIFICATION_BACKEND", "test")
	t.Setenv("MULTICA_VERIFICATION_TOKEN", strings.Repeat("a", 64))
	t.Setenv("MULTICA_VERIFICATION_DIRECTORY", t.TempDir())
	router := chi.NewRouter()
	testHandler.RegisterVerificationRoutes(router)
	testutil.Call(t, router.ServeHTTP, newRequest("GET", "/api/verification/capabilities", nil)).Want(404)
	testutil.Call(t, router.ServeHTTP, newRequest("POST", "/api/verification/import", map[string]any{})).Want(404)
}
