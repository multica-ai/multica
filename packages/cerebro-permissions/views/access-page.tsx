"use client";

import { useState } from "react";
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
import { SubjectsTab } from "./components/subjects-tab";
import { GrantsToolbar } from "./components/grants-toolbar";
import { GrantsTable } from "./components/grants-table";
import { GrantDrawer } from "./components/grant-drawer";
import { CreateGrantDialog } from "./components/create-grant-dialog";
import { AuditTab } from "./components/audit-tab";
import { AuthoringMatrixTab } from "./components/authoring-matrix-tab";
import { RolesTab } from "./components/roles-tab";
import { EffectivePermissionTab } from "./components/effective-permission-tab";

type PageTab = "subjects" | "grants" | "matrix" | "roles" | "effective" | "audit";

export function AccessPage() {
  const workspace = useCurrentWorkspace();
  const { role, isLoading: isMemberLoading } = useCurrentMember(workspace?.id ?? "");

  const [activeTab, setActiveTab] = useState<PageTab>("subjects");
  const [showCreate, setShowCreate] = useState(false);
  const [selectedGrantId, setSelectedGrantId] = useState<string | null>(null);

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
              <strong className="text-foreground">People &amp; Agents</strong> — pick an agent or person and see what it can do. <em>Expand a row → flip the &quot;Ask first&quot; switch</em> to make it ask a human before using that capability.{" "}
              <strong className="text-foreground">Permissions</strong> — create or edit grants (the base allow/deny rules).{" "}
              <strong className="text-foreground">Matrix</strong> — the same grants as a who-can-do-what grid.{" "}
              <strong className="text-foreground">Roles</strong> — reusable bundles of grants you assign to people or agents.{" "}
              <strong className="text-foreground">Effective</strong> — check exactly what one subject may do on a resource.{" "}
              <strong className="text-foreground">Audit</strong> — every change, with who/when/what.{" "}
              Requests waiting for a decision live in <strong className="text-foreground">Approvals</strong> in the sidebar.
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
            <TabsTrigger value="grants">Permissions</TabsTrigger>
            <TabsTrigger value="matrix">Matrix</TabsTrigger>
            <TabsTrigger value="roles">Roles</TabsTrigger>
            <TabsTrigger value="effective">Effective</TabsTrigger>
            <TabsTrigger value="audit">Audit</TabsTrigger>
          </TabsList>
        </div>

        <TabsContent value="subjects" className="flex flex-1 min-h-0 flex-col">
          <SubjectsTab wsId={wsId} />
        </TabsContent>

        <TabsContent value="grants" className="flex flex-1 min-h-0 flex-col">
          <GrantsToolbar wsId={wsId} onCreate={() => setShowCreate(true)} />
          <div className="flex-1 min-h-0 overflow-y-auto">
            <GrantsTable wsId={wsId} onRowClick={(id) => setSelectedGrantId(id)} />
          </div>
        </TabsContent>

        <TabsContent value="matrix" className="flex flex-1 min-h-0 flex-col">
          <AuthoringMatrixTab wsId={wsId} />
        </TabsContent>

        <TabsContent value="roles" className="flex flex-1 min-h-0 flex-col">
          <RolesTab wsId={wsId} />
        </TabsContent>

        <TabsContent value="effective" className="flex flex-1 min-h-0 flex-col">
          <EffectivePermissionTab wsId={wsId} />
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
