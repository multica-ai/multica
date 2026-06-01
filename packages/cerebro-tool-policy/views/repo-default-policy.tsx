"use client";

// Repo default-access controls (FIR-2505). A workspace owner picks Allow / Ask /
// Deny / No default for a repo; that choice is written as the WORKSPACE-layer
// tool-policy for all three repo capabilities (read / checkout / push) at once,
// scoped to the repo URL — the same rows the permission screen reads, so the two
// stay in sync.
//
// Per Jesper (FIR-2505): the Repositories tab only *shows* the current default
// and links out to the Permissions screen to manage it; the one place a default
// is *set* is when a new repo is added. So this module exposes:
//   - RepoDefaultPolicySelect — a pure controlled picker used in the add-repo form.
//   - useWriteRepoDefaultPolicy — writes the chosen default on save.
//   - RepoPolicyBadge — read-only current default + a "manage in Permissions" link.
//
// Enforcement of the choice is gated by the daemon repo check (FIR-2505
// enforcement slice); "Ask" begins to actually seek approval once the shared
// approval path lands (FIR-2586).

import { useCallback, useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { SlidersHorizontal } from "lucide-react";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import {
  toolPolicyTableOptions,
  useClearToolPolicy,
  useSetToolPolicy,
  type ToolSetting,
} from "../core";

export type { ToolSetting } from "../core";

// The three repo capabilities a single workspace-level default fans out to.
const REPO_CAPS = ["repo.read", "repo.checkout", "repo.push"] as const;

// "inherit" is surfaced as "No default" — it clears the workspace layer rather
// than storing an explicit inherit row, matching the permission screen.
const CHOICES: { value: ToolSetting; label: string }[] = [
  { value: "inherit", label: "No default" },
  { value: "allow", label: "Allow" },
  { value: "ask", label: "Ask" },
  { value: "deny", label: "Deny" },
];

function labelFor(setting: ToolSetting): string {
  return CHOICES.find((c) => c.value === setting)?.label ?? "No default";
}

// Reads the current workspace-layer default for a repo. repo.checkout is the
// representative capability — the three move together when set from here.
function useRepoDefault(wsId: string, repoUrl: string): ToolSetting {
  const query = useQuery(toolPolicyTableOptions(wsId, {}));
  return useMemo<ToolSetting>(() => {
    const rows = query.data ?? [];
    const row = rows.find(
      (r) => r.tool_key === "repo.checkout" && r.resource_pattern === repoUrl,
    );
    return row?.layers.workspace ?? "inherit";
  }, [query.data, repoUrl]);
}

// RepoDefaultPolicySelect is a pure, controlled picker. The add-repo form owns
// the draft value and persists it on save via useWriteRepoDefaultPolicy — so no
// tool-policy row is written for a repo until the repo itself is saved.
export function RepoDefaultPolicySelect({
  value,
  onChange,
  disabled,
}: {
  value: ToolSetting;
  onChange: (value: ToolSetting) => void;
  disabled?: boolean;
}) {
  return (
    <Select value={value} onValueChange={(v) => onChange(v as ToolSetting)} disabled={disabled}>
      <SelectTrigger
        className="h-9 w-full text-sm"
        aria-label="Default repo access"
        data-testid="repo-default-policy-select"
      >
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {CHOICES.map((c) => (
          <SelectItem key={c.value} value={c.value} className="text-xs">
            {c.label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

// useWriteRepoDefaultPolicy returns a writer that fans a single default out to
// the three repo capabilities at the workspace layer (or clears them for the
// "No default" choice). Used by the add-repo form on save.
export function useWriteRepoDefaultPolicy() {
  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();
  return useCallback(
    (wsId: string, repoUrl: string, setting: ToolSetting) => {
      if (!repoUrl) return;
      for (const cap of REPO_CAPS) {
        if (setting === "inherit") {
          clearPolicy.mutate({
            tool_key: cap,
            layer: "workspace",
            subject_id: wsId,
            resource_pattern: repoUrl,
          });
        } else {
          setPolicy.mutate({
            tool_key: cap,
            layer: "workspace",
            subject_id: wsId,
            setting,
            resource_pattern: repoUrl,
          });
        }
      }
    },
    [setPolicy, clearPolicy],
  );
}

// RepoPolicyBadge shows a saved repo's current default access read-only, plus a
// link that takes the owner to the Permissions screen to actually change it.
export function RepoPolicyBadge({
  wsId,
  repoUrl,
  onManage,
}: {
  wsId: string;
  repoUrl: string;
  onManage: () => void;
}) {
  const current = useRepoDefault(wsId, repoUrl);
  if (!repoUrl) return null;
  return (
    <button
      type="button"
      onClick={onManage}
      title="Manage repo access in Permissions"
      data-testid={`repo-policy-badge-${repoUrl}`}
      className="flex h-8 items-center gap-1.5 rounded-md border bg-muted/30 px-2 text-xs text-muted-foreground transition-colors hover:bg-muted hover:text-foreground"
    >
      <span>{labelFor(current)}</span>
      <SlidersHorizontal className="h-3 w-3" />
    </button>
  );
}
