"use client";

import { useState } from "react";
import { Plus } from "lucide-react";
import type { RuntimeGroup, RuntimeDevice, MemberWithUser } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { CreateRuntimeGroupDialog } from "./create-runtime-group-dialog";

export function RuntimeGroupsPage({
  groups,
  runtimes,
  members,
  currentUserId,
  onCreate,
  onOpenGroup,
}: {
  groups: RuntimeGroup[];
  runtimes: RuntimeDevice[];
  members: MemberWithUser[];
  currentUserId: string | null;
  onCreate: (req: { name: string; description: string; runtime_ids: string[] }) => Promise<void>;
  onOpenGroup: (id: string) => void;
}) {
  const [creating, setCreating] = useState(false);

  return (
    <div className="p-6 max-w-4xl">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-lg font-semibold">Runtime Groups</h1>
        <Button size="sm" onClick={() => setCreating(true)}>
          <Plus className="h-3.5 w-3.5 mr-1.5" />
          New Group
        </Button>
      </div>

      {groups.length === 0 ? (
        <div className="text-sm text-muted-foreground">
          No runtime groups yet. Create one to let agents share a reusable runtime set.
        </div>
      ) : (
        <div className="space-y-2">
          {groups.map((g) => (
            <button
              key={g.id}
              onClick={() => onOpenGroup(g.id)}
              className="w-full flex items-center justify-between rounded-lg border border-border bg-background px-4 py-3 text-left hover:bg-muted transition-colors"
            >
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="truncate font-medium">{g.name}</span>
                  {g.active_override && (
                    <span className="shrink-0 rounded bg-amber-500/10 px-1.5 py-0.5 text-xs font-medium text-amber-600">
                      Override active
                    </span>
                  )}
                </div>
                <div className="mt-0.5 truncate text-xs text-muted-foreground">
                  {g.runtimes.length} runtime{g.runtimes.length === 1 ? "" : "s"}
                  {" · "}
                  {g.member_agent_count} agent{g.member_agent_count === 1 ? "" : "s"}
                </div>
              </div>
            </button>
          ))}
        </div>
      )}

      {creating && (
        <CreateRuntimeGroupDialog
          runtimes={runtimes}
          members={members}
          currentUserId={currentUserId}
          onClose={() => setCreating(false)}
          onCreate={async (req) => {
            await onCreate(req);
            setCreating(false);
          }}
        />
      )}
    </div>
  );
}
