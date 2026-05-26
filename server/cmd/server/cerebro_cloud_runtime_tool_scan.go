package main

// CEREBRO-PATCH(cloud-runtime-tool-scan-meta): FIR-2284 — build the callable
// built-in tool list the cloud-runtime "Scan now" records. Mirrors the cloud
// tool seed filter (implemented + newly_implemented) so a cloud-runtime scan
// surfaces exactly the gateway tools an agent can actually call, never the
// explicitly-excluded ones.

import (
	"github.com/multica-ai/multica/server/internal/cerebro/cloudtoolscan"
	cerebroruntime "github.com/multica-ai/multica/server/internal/cerebro/runtime"
)

func callableCloudToolMeta() []cloudtoolscan.ToolMeta {
	meta := cerebroruntime.AllBuiltinToolMeta()
	out := make([]cloudtoolscan.ToolMeta, 0, len(meta))
	for _, m := range meta {
		if m.Status != cerebroruntime.ToolStatusImplemented &&
			m.Status != cerebroruntime.ToolStatusNewlyImplemented {
			continue
		}
		out = append(out, cloudtoolscan.ToolMeta{Name: m.Name, Description: m.Description})
	}
	return out
}
