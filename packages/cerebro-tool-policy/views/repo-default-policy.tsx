"use client";

// RepoDefaultPolicy is the per-repo "default access" control a workspace owner
// sets when managing repositories (FIR-2505): pick Allow / Ask / Deny for a repo
// and that choice is written as the WORKSPACE-layer tool-policy for all three
// repo capabilities (read / checkout / push) at once, scoped to the repo URL.
//
// It is the same workspace-layer row the permission screen renders, so the two
// stay in sync: the owner can set a coarse default here on add, then refine per
// capability / per agent in the screen. "No default" clears the workspace layer
// so the repo falls back to the base. Enforcement of the choice is gated by the
// daemon repo check (FIR-2505 enforcement slice); "Ask" begins to actually seek
// approval once the shared approval path lands (FIR-2586).

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
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

export function RepoDefaultPolicy({ wsId, repoUrl }: { wsId: string; repoUrl: string }) {
  const query = useQuery(toolPolicyTableOptions(wsId, {}));
  const setPolicy = useSetToolPolicy();
  const clearPolicy = useClearToolPolicy();

  // repo.checkout is the representative capability; the three move together when
  // set from here, so its workspace-layer value reflects the current default.
  const current = useMemo<ToolSetting>(() => {
    const rows = query.data ?? [];
    const row = rows.find(
      (r) => r.tool_key === "repo.checkout" && r.resource_pattern === repoUrl,
    );
    return row?.layers.workspace ?? "inherit";
  }, [query.data, repoUrl]);

  if (!repoUrl) return null;
  const busy = setPolicy.isPending || clearPolicy.isPending;

  function apply(value: ToolSetting) {
    for (const cap of REPO_CAPS) {
      if (value === "inherit") {
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
          setting: value,
          resource_pattern: repoUrl,
        });
      }
    }
  }

  return (
    <Select value={current} onValueChange={(v) => apply(v as ToolSetting)} disabled={busy}>
      <SelectTrigger
        className="h-8 w-[116px] shrink-0 text-xs"
        aria-label="Default repo access"
        data-testid={`repo-default-policy-${repoUrl}`}
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
