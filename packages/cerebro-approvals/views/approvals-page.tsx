"use client";

import { useCallback, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { Inbox, ShieldOff } from "lucide-react";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { useWSEvent } from "@multica/core/realtime";
import {
  Tabs,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { PageHeader } from "@multica/views/layout/page-header";
import { approvalKeys } from "../core/queries";
import { ApprovalList } from "./components/approval-list";
import { ApprovalDetail } from "./components/approval-detail";

type Filter = "pending" | "all";

// Approval inbox (FIR-2131): one list of asks the permission engine flagged
// needs_approval. Admins approve / reject / delegate. A WS event fires an
// in-app toast and refreshes the cache when a new ask lands.
export function ApprovalsPage() {
  const workspace = useCurrentWorkspace();
  const { role, isLoading: memberLoading } = useCurrentMember(workspace?.id ?? "");
  const qc = useQueryClient();

  const [filter, setFilter] = useState<Filter>("pending");
  const [selectedId, setSelectedId] = useState<string | null>(null);

  const wsId = workspace?.id ?? "";

  const invalidate = useCallback(() => {
    if (wsId) qc.invalidateQueries({ queryKey: approvalKeys.all(wsId) });
  }, [qc, wsId]);

  useWSEvent(
    "approval:created",
    useCallback(() => {
      toast.info("New request in the approval inbox", { description: "Requires approval" });
      invalidate();
    }, [invalidate]),
  );
  useWSEvent("approval:decided", invalidate);
  useWSEvent("approval:delegated", invalidate);
  useWSEvent("approval:expired", invalidate);

  if (!workspace || memberLoading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Loading workspace…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Access denied</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Only workspace owners and admins can handle approval requests.
        </p>
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3 px-5">
        <div className="flex min-w-0 items-center gap-2">
          <Inbox className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Approvals</h1>
          <p className="ml-2 hidden truncate text-xs text-muted-foreground md:block">
            Requests waiting on a human decision
          </p>
        </div>
      </PageHeader>

      <Tabs
        value={filter}
        onValueChange={(v) => setFilter(v as Filter)}
        className="flex flex-1 min-h-0 flex-col"
      >
        <div className="border-b px-4">
          <TabsList variant="line" className="h-10">
            <TabsTrigger value="pending">Pending</TabsTrigger>
            <TabsTrigger value="all">All</TabsTrigger>
          </TabsList>
        </div>

        <div className="flex-1 min-h-0 overflow-y-auto">
          <ApprovalList
            wsId={wsId}
            status={filter === "pending" ? "pending" : null}
            selectedId={selectedId}
            onSelect={setSelectedId}
          />
        </div>
      </Tabs>

      {selectedId && (
        <ApprovalDetail
          wsId={wsId}
          approvalId={selectedId}
          onClose={() => setSelectedId(null)}
        />
      )}
    </div>
  );
}
