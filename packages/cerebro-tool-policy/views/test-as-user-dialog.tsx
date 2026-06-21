"use client";

// FIR-1771 "Test as user" — resolve, from inside the app, what a chosen
// user + agent combination is Allowed / Asked / Denied for one tool. It is the
// in-app equivalent of `multica tool-policy explain`, and it auto-resolves the
// target user's real group membership server-side. Opened from the profile menu;
// only rendered for members the tools:test-as-user capability allows.

import { useMemo, useState } from "react";
import { Loader2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Label } from "@multica/ui/components/ui/label";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { runTestAsUser, type TestAsUserTool } from "../api";

type Verdict = { label: string; cls: string };

// verdictFor maps a resolved setting to a human label + a semantic-token class.
// Unknown settings downgrade to a neutral "Inherit" rather than crashing.
function verdictFor(setting: string): Verdict {
  switch (setting) {
    case "allow":
      return { label: "Allow", cls: "bg-emerald-500/15 text-emerald-600" };
    case "ask":
      return { label: "Ask", cls: "bg-amber-500/15 text-amber-600" };
    case "deny":
      return { label: "Deny", cls: "bg-destructive/15 text-destructive" };
    default:
      return { label: "Inherit", cls: "bg-muted text-muted-foreground" };
  }
}

export function TestAsUserDialog({
  wsId,
  open,
  onOpenChange,
}: {
  wsId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const [userId, setUserId] = useState("");
  const [agentId, setAgentId] = useState("");
  const [toolKey, setToolKey] = useState("");

  const membersQuery = useQuery({ ...memberListOptions(wsId), enabled: open });
  const agentsQuery = useQuery({ ...agentListOptions(wsId), enabled: open });

  const runtimeId = useMemo(
    () => agentsQuery.data?.find((a) => a.id === agentId)?.runtime_id ?? "",
    [agentsQuery.data, agentId],
  );

  // Resolve the whole context once both a user and an agent are chosen; the tool
  // dropdown is then populated from the result and the verdict is read locally.
  const resultQuery = useQuery({
    queryKey: ["cerebro", "test-as-user", wsId, userId, agentId],
    queryFn: () => runTestAsUser(wsId, { userId, agentId, runtimeId }),
    enabled: open && !!userId && !!agentId,
  });

  const tools: TestAsUserTool[] = resultQuery.data ?? [];
  const selectedTool = useMemo(
    () => tools.find((t) => t.tool_key === toolKey),
    [tools, toolKey],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>Test as user</DialogTitle>
          <DialogDescription>
            Look up what a user + agent is allowed, asked or denied for a tool —
            the same answer the permission engine gives at run time.
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-4 py-2">
          <div className="flex flex-col gap-1.5">
            <Label>User</Label>
            <Select
              value={userId}
              onValueChange={(v) => {
                setUserId(v ?? "");
                setToolKey("");
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select a user" />
              </SelectTrigger>
              <SelectContent>
                {(membersQuery.data ?? []).map((m) => (
                  <SelectItem key={m.user_id} value={m.user_id}>
                    {m.name || m.email}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label>Agent</Label>
            <Select
              value={agentId}
              onValueChange={(v) => {
                setAgentId(v ?? "");
                setToolKey("");
              }}
            >
              <SelectTrigger>
                <SelectValue placeholder="Select an agent" />
              </SelectTrigger>
              <SelectContent>
                {(agentsQuery.data ?? []).map((a) => (
                  <SelectItem key={a.id} value={a.id}>
                    {a.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label>Tool</Label>
            <Select
              value={toolKey}
              onValueChange={(v) => setToolKey(v ?? "")}
              disabled={!userId || !agentId || resultQuery.isLoading}
            >
              <SelectTrigger>
                <SelectValue
                  placeholder={
                    resultQuery.isLoading ? "Resolving…" : "Select a tool"
                  }
                />
              </SelectTrigger>
              <SelectContent>
                {tools.map((t) => (
                  <SelectItem key={t.tool_key} value={t.tool_key}>
                    {t.title || t.tool_key}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          {resultQuery.isLoading && (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> Resolving permissions…
            </div>
          )}

          {selectedTool && (
            <VerdictCard tool={selectedTool} />
          )}
        </div>

        <div className="flex justify-end">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function VerdictCard({ tool }: { tool: TestAsUserTool }) {
  const v = verdictFor(tool.effective?.setting ?? "");
  const decidedBy = tool.effective?.capped_by || tool.effective?.decided_by;
  const cappedGroups = (tool.capped_by_groups ?? [])
    .map((g) => g.name)
    .filter(Boolean);
  return (
    <div className="flex flex-col gap-2 rounded-md border border-border p-3">
      <div className="flex items-center justify-between">
        <span className="font-medium">{tool.title || tool.tool_key}</span>
        <span
          className={`rounded px-2 py-0.5 text-xs font-semibold ${v.cls}`}
        >
          {v.label}
        </span>
      </div>
      {decidedBy && (
        <div className="text-sm text-muted-foreground">
          Decided by: <span className="text-foreground">{decidedBy} layer</span>
          {tool.effective?.capped_by ? " (cap)" : ""}
        </div>
      )}
      {cappedGroups.length > 0 && (
        <div className="text-sm text-muted-foreground">
          Capped by group{cappedGroups.length > 1 ? "s" : ""}:{" "}
          <span className="text-foreground">{cappedGroups.join(", ")}</span>
        </div>
      )}
      {tool.effective?.reason && (
        <div className="text-sm text-muted-foreground">
          {tool.effective.reason}
        </div>
      )}
    </div>
  );
}
