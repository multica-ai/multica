"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Info, ShieldCheck, ShieldOff } from "lucide-react";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { PageHeader } from "@multica/views/layout/page-header";
import { pendingAsksOptions } from "../core";
import { SubjectsTab } from "./components/subjects-tab";
import { PendingAsksTab } from "./components/pending-asks-tab";
import { GrantsToolbar } from "./components/grants-toolbar";
import { GrantsTable } from "./components/grants-table";
import { GrantDrawer } from "./components/grant-drawer";
import { CreateGrantDialog } from "./components/create-grant-dialog";
import { AuditTab } from "./components/audit-tab";

type PageTab = "subjects" | "pending" | "grants" | "audit";

export function AccessPage() {
  const workspace = useCurrentWorkspace();
  const { role, isLoading: isMemberLoading } = useCurrentMember(workspace?.id ?? "");

  const [activeTab, setActiveTab] = useState<PageTab>("subjects");
  const [showCreate, setShowCreate] = useState(false);
  const [selectedGrantId, setSelectedGrantId] = useState<string | null>(null);

  const pendingCount = useQuery(
    pendingAsksOptions(workspace?.id ?? "", { limit: 1, offset: 0 }),
  );

  if (!workspace || isMemberLoading) {
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
          Only workspace owners and admins can manage access and view the audit log.
        </p>
      </div>
    );
  }

  const wsId = workspace.id;
  const pendingTotal = pendingCount.data?.total ?? 0;

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between gap-3 px-5">
        <div className="flex min-w-0 items-center gap-2">
          <ShieldCheck className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Access</h1>
          <p className="ml-2 hidden truncate text-xs text-muted-foreground md:block">
            Who can do what — and what needs your approval first.
          </p>
        </div>
      </PageHeader>

      <div className="border-b bg-muted/30 px-5 py-3 text-xs text-muted-foreground">
        <div className="flex items-start gap-2">
          <Info className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
          <div className="space-y-1">
            <p>
              <strong className="text-foreground">People &amp; Agents</strong> — see who has access to what, expand a row to view their capabilities.{" "}
              <strong className="text-foreground">Pending</strong> — requests waiting for you to approve or reject.{" "}
              <strong className="text-foreground">Permissions</strong> — create or edit grants; flip <em>Requires approval</em> to make a subject ask you first.{" "}
              <strong className="text-foreground">Audit</strong> — every change to permissions, with who/when/what.
            </p>
          </div>
        </div>
      </div>

      <Tabs
        value={activeTab}
        onValueChange={(v) => setActiveTab(v as PageTab)}
        className="flex flex-1 min-h-0 flex-col"
      >
        <div className="border-b px-4">
          <TabsList variant="line" className="h-10">
            <TabsTrigger value="subjects">People &amp; Agents</TabsTrigger>
            <TabsTrigger value="pending" className="relative">
              Pending
              {pendingTotal > 0 && (
                <span className="ml-1.5 inline-flex h-4 min-w-[16px] items-center justify-center rounded-full bg-warning text-[10px] font-semibold text-warning-foreground px-1">
                  {pendingTotal > 99 ? "99+" : pendingTotal}
                </span>
              )}
            </TabsTrigger>
            <TabsTrigger value="grants">Permissions</TabsTrigger>
            <TabsTrigger value="audit">Audit</TabsTrigger>
          </TabsList>
        </div>

        <TabsContent value="subjects" className="flex flex-1 min-h-0 flex-col">
          <SubjectsTab wsId={wsId} />
        </TabsContent>

        <TabsContent value="pending" className="flex flex-1 min-h-0 flex-col">
          <PendingAsksTab wsId={wsId} />
        </TabsContent>

        <TabsContent value="grants" className="flex flex-1 min-h-0 flex-col">
          <GrantsToolbar wsId={wsId} onCreate={() => setShowCreate(true)} />
          <div className="flex-1 min-h-0 overflow-y-auto">
            <GrantsTable wsId={wsId} onRowClick={(id) => setSelectedGrantId(id)} />
          </div>
        </TabsContent>

        <TabsContent value="audit" className="flex flex-1 min-h-0 flex-col">
          <AuditTab wsId={wsId} />
        </TabsContent>
      </Tabs>

      {showCreate && (
        <CreateGrantDialog wsId={wsId} onClose={() => setShowCreate(false)} />
      )}

      {selectedGrantId && (
        <GrantDrawer
          wsId={wsId}
          grantId={selectedGrantId}
          onClose={() => setSelectedGrantId(null)}
        />
      )}
    </div>
  );
}
