import { useState } from "react";
import type { AgentRuntime } from "@/shared/types";
import { formatLastSeen } from "../utils";
import { RuntimeModeIcon, StatusBadge, InfoField } from "./shared";
import { PingSection } from "./ping-section";
import { UpdateSection } from "./update-section";
import { UsageSection } from "./usage-section";
import { useAuthStore } from "@/features/auth";
import { useRuntimeStore } from "../store";
import { Globe, Lock } from "lucide-react";
import { api } from "@/shared/api";
import { toast } from "sonner";

function getCliVersion(metadata: Record<string, unknown>): string | null {
  if (
    metadata &&
    typeof metadata.cli_version === "string" &&
    metadata.cli_version
  ) {
    return metadata.cli_version;
  }
  return null;
}

function VisibilitySection({ runtime }: { runtime: AgentRuntime }) {
  const userId = useAuthStore((s) => s.user?.id);
  const isOwner = runtime.owner_id === userId;
  const [saving, setSaving] = useState(false);

  if (!isOwner) {
    return (
      <div>
        <h3 className="text-xs font-medium text-muted-foreground mb-3">Visibility</h3>
        <div className="flex items-center gap-2 text-sm text-muted-foreground">
          {runtime.visibility === "private" ? <Lock className="h-4 w-4" /> : <Globe className="h-4 w-4" />}
          {runtime.visibility === "private" ? "Private" : "Workspace"}
        </div>
      </div>
    );
  }

  const handleToggle = async (vis: "workspace" | "private") => {
    if (vis === runtime.visibility || saving) return;
    setSaving(true);
    try {
      const updated = await api.updateRuntime(runtime.id, { visibility: vis });
      useRuntimeStore.getState().patchRuntime(runtime.id, updated);
      toast.success(`Runtime visibility set to ${vis}`);
    } catch {
      toast.error("Failed to update visibility");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div>
      <h3 className="text-xs font-medium text-muted-foreground mb-3">Visibility</h3>
      <div className="flex gap-2">
        <button
          type="button"
          onClick={() => handleToggle("workspace")}
          disabled={saving}
          className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
            runtime.visibility === "workspace" ? "border-primary bg-primary/5" : "border-border hover:bg-muted"
          }`}
        >
          <Globe className="h-4 w-4 shrink-0 text-muted-foreground" />
          <div className="text-left">
            <div className="font-medium">Workspace</div>
            <div className="text-xs text-muted-foreground">All members can see</div>
          </div>
        </button>
        <button
          type="button"
          onClick={() => handleToggle("private")}
          disabled={saving}
          className={`flex flex-1 items-center gap-2 rounded-lg border px-3 py-2.5 text-sm transition-colors ${
            runtime.visibility === "private" ? "border-primary bg-primary/5" : "border-border hover:bg-muted"
          }`}
        >
          <Lock className="h-4 w-4 shrink-0 text-muted-foreground" />
          <div className="text-left">
            <div className="font-medium">Private</div>
            <div className="text-xs text-muted-foreground">Only you can see</div>
          </div>
        </button>
      </div>
    </div>
  );
}

export function RuntimeDetail({ runtime }: { runtime: AgentRuntime }) {
  const cliVersion =
    runtime.runtime_mode === "local" ? getCliVersion(runtime.metadata) : null;

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <div className="flex min-w-0 items-center gap-2">
          <div
            className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-md ${
              runtime.status === "online" ? "bg-success/10" : "bg-muted"
            }`}
          >
            <RuntimeModeIcon mode={runtime.runtime_mode} />
          </div>
          <div className="min-w-0">
            <h2 className="text-sm font-semibold truncate">{runtime.name}</h2>
          </div>
        </div>
        <StatusBadge status={runtime.status} />
      </div>

      {/* Content */}
      <div className="flex-1 overflow-y-auto p-6 space-y-6">
        {/* Info grid */}
        <div className="grid grid-cols-2 gap-4">
          <InfoField label="Runtime Mode" value={runtime.runtime_mode} />
          <InfoField label="Provider" value={runtime.provider} />
          <InfoField label="Status" value={runtime.status} />
          <InfoField
            label="Last Seen"
            value={formatLastSeen(runtime.last_seen_at)}
          />
          {runtime.device_info && (
            <InfoField label="Device" value={runtime.device_info} />
          )}
          {runtime.daemon_id && (
            <InfoField label="Daemon ID" value={runtime.daemon_id} mono />
          )}
        </div>

        {/* Visibility (owner only) */}
        <VisibilitySection runtime={runtime} />

        {/* CLI Version & Update */}
        {runtime.runtime_mode === "local" && (
          <div>
            <h3 className="text-xs font-medium text-muted-foreground mb-3">
              CLI Version
            </h3>
            <UpdateSection
              runtimeId={runtime.id}
              currentVersion={cliVersion}
              isOnline={runtime.status === "online"}
            />
          </div>
        )}

        {/* Connection Test */}
        <div>
          <h3 className="text-xs font-medium text-muted-foreground mb-3">
            Connection Test
          </h3>
          <PingSection runtimeId={runtime.id} />
        </div>

        {/* Usage */}
        <div>
          <h3 className="text-xs font-medium text-muted-foreground mb-3">
            Token Usage
          </h3>
          <UsageSection runtimeId={runtime.id} />
        </div>

        {/* Metadata */}
        {runtime.metadata && Object.keys(runtime.metadata).length > 0 && (
          <div>
            <h3 className="text-xs font-medium text-muted-foreground mb-2">
              Metadata
            </h3>
            <div className="rounded-lg border bg-muted/30 p-3">
              <pre className="text-xs font-mono whitespace-pre-wrap break-all">
                {JSON.stringify(runtime.metadata, null, 2)}
              </pre>
            </div>
          </div>
        )}

        {/* Timestamps */}
        <div className="grid grid-cols-2 gap-4 border-t pt-4">
          <InfoField
            label="Created"
            value={new Date(runtime.created_at).toLocaleString()}
          />
          <InfoField
            label="Updated"
            value={new Date(runtime.updated_at).toLocaleString()}
          />
        </div>
      </div>
    </div>
  );
}
