package main

// CEREBRO-PATCH(router-infisical-provisioner): FIR-2192 — wire the scoped-
// per-user Infisical machine identity provisioner from env. Returns nil when
// the operator hasn't configured Infisical, so dev + self-host instances
// without the integration still boot.

import (
	"log/slog"
	"os"
	"strings"

	cerebrocredentials "github.com/multica-ai/multica/server/internal/cerebro/credentials"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	cerebroinfisical "github.com/multica-ai/multica/server/internal/cerebro/infisical"
)

func newInfisicalProvisioner(queries *cerebrodb.Queries, cipher *cerebrocredentials.Cipher) *cerebroinfisical.Provisioner {
	domain := strings.TrimSpace(os.Getenv("INFISICAL_DOMAIN"))
	orgID := strings.TrimSpace(os.Getenv("INFISICAL_ORG_ID"))
	projectID := strings.TrimSpace(os.Getenv("INFISICAL_PROJECT_ID"))
	clientID := strings.TrimSpace(os.Getenv("INFISICAL_ADMIN_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("INFISICAL_ADMIN_CLIENT_SECRET"))

	if domain == "" || orgID == "" || projectID == "" || clientID == "" || clientSecret == "" {
		// Partial config is almost always an operator typo. Log loud so they
		// see it in startup output without taking the boot down with us.
		anySet := domain != "" || orgID != "" || projectID != "" || clientID != "" || clientSecret != ""
		if anySet {
			slog.Warn("infisical provisioner disabled: incomplete env configuration",
				"INFISICAL_DOMAIN_set", domain != "",
				"INFISICAL_ORG_ID_set", orgID != "",
				"INFISICAL_PROJECT_ID_set", projectID != "",
				"INFISICAL_ADMIN_CLIENT_ID_set", clientID != "",
				"INFISICAL_ADMIN_CLIENT_SECRET_set", clientSecret != "",
			)
		}
		return nil
	}
	if cipher == nil {
		slog.Warn("infisical provisioner disabled: MULTICA_CREDENTIALS_KEY is not configured")
		return nil
	}

	slog.Info("infisical provisioner enabled", "domain", domain, "project_id", projectID)
	return &cerebroinfisical.Provisioner{
		Admin: &cerebroinfisical.AdminClient{
			Domain:       domain,
			OrgID:        orgID,
			ProjectID:    projectID,
			ClientID:     clientID,
			ClientSecret: clientSecret,
		},
		Queries: queries,
		Cipher:  cipher,
	}
}
