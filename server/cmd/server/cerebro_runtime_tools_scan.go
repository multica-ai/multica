package main

// CEREBRO-PATCH(router-runtime-tools-scan): JEH-1710 daemon-side scan ingest
// adapter. Keeps the handler package free of cerebrodb generated types by
// translating handler view structs into runtimetools domain types.

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/runtimetools"
	"github.com/multica-ai/multica/server/internal/handler"
)

type runtimeToolsScanAdapter struct {
	svc *runtimetools.Service
}

func newRuntimeToolsScanAdapter(svc *runtimetools.Service) *runtimeToolsScanAdapter {
	return &runtimeToolsScanAdapter{svc: svc}
}

func (a *runtimeToolsScanAdapter) RecordScan(ctx context.Context, runtimeID pgtype.UUID, scannedAt time.Time, servers []handler.RuntimeToolScanServer) error {
	domain := make([]runtimetools.ScannedServer, 0, len(servers))
	for _, s := range servers {
		tools := make([]runtimetools.ScannedTool, 0, len(s.Tools))
		for _, t := range s.Tools {
			tools = append(tools, runtimetools.ScannedTool{
				Name:        t.Name,
				Description: t.Description,
				SchemaJSON:  t.InputSchema,
			})
		}
		domain = append(domain, runtimetools.ScannedServer{
			Name:  s.Name,
			Tools: tools,
			Error: s.Error,
		})
	}
	stamp := pgtype.Timestamptz{Time: scannedAt, Valid: !scannedAt.IsZero()}
	return a.svc.RecordScan(ctx, runtimeID, stamp, domain)
}
