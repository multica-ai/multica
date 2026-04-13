import { useState } from "react";
import { CheckCircle2, XCircle, Loader2, Search } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { sandboxConfigListOptions, workspaceKeys } from "@multica/core/workspace/queries";
import type { ProviderDetection } from "@multica/core/types";
import { toast } from "sonner";
import { ProviderLogo } from "./provider-logo";

export function ProviderPanel({ runtimeId, sandboxConfigId }: { runtimeId: string; sandboxConfigId?: string | null }) {
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const { data: configs = [] } = useQuery(sandboxConfigListOptions(wsId));
  const cfg = sandboxConfigId ? configs.find((c) => c.id === sandboxConfigId) : null;

  // Read cached detection from sandbox config metadata
  const templateId = cfg?.template_id ?? "base";
  const cached = cfg?.metadata?.detected_providers as Record<string, { providers: ProviderDetection[]; detected_at: string }> | undefined;
  const cachedEntry = cached?.[templateId];

  const [detecting, setDetecting] = useState(false);
  const [freshResults, setFreshResults] = useState<ProviderDetection[] | null>(null);

  // Use fresh results if available, otherwise fall back to cached
  const results = freshResults ?? cachedEntry?.providers ?? null;

  const handleDetect = async () => {
    setDetecting(true);
    setFreshResults(null);
    try {
      const data = await api.detectProviders(runtimeId);
      setFreshResults(data);
      // Invalidate sandbox configs so cached metadata is refreshed for next mount
      qc.invalidateQueries({ queryKey: workspaceKeys.sandboxConfigs(wsId) });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Detection failed");
    } finally {
      setDetecting(false);
    }
  };

  const hasResults = results !== null && !detecting;

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-muted-foreground">Agent Providers</h3>
        <Button variant="outline" size="xs" onClick={handleDetect} disabled={detecting}>
          {detecting ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Search className="h-3 w-3" />
          )}
          {detecting ? "Detecting..." : hasResults ? "Re-detect" : "Detect"}
        </Button>
      </div>

      {cachedEntry && !freshResults && !detecting && (
        <p className="text-xs text-muted-foreground">
          Last detected: {new Date(cachedEntry.detected_at).toLocaleString()}
        </p>
      )}

      {results === null && !detecting ? (
        <p className="text-xs text-muted-foreground">
          Click Detect to check which agent tools are installed in the sandbox template.
        </p>
      ) : results !== null ? (
        <div className="space-y-2">
          {results.map((p) => (
            <div
              key={p.name}
              className="flex items-center justify-between rounded-lg border px-3 py-2"
            >
              <div className="flex items-center gap-2">
                <ProviderLogo provider={p.name} className="h-4 w-4" />
                <span className="text-sm font-medium">{p.display}</span>
                {p.installed ? (
                  <span className="inline-flex items-center gap-1 text-xs text-success">
                    <CheckCircle2 className="h-3 w-3" />
                    Installed
                  </span>
                ) : (
                  <span className="inline-flex items-center gap-1 text-xs text-muted-foreground">
                    <XCircle className="h-3 w-3" />
                    Not found
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}
