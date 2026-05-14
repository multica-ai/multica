"use client";

import { useState } from "react";
import { ShieldCheck, ShieldOff } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { PageHeader } from "@multica/views/layout/page-header";
import { GrantsToolbar } from "./components/grants-toolbar";
import { GrantsTable } from "./components/grants-table";
import { GrantDrawer } from "./components/grant-drawer";
import { CreateGrantDialog } from "./components/create-grant-dialog";
import { AuditTab } from "./components/audit-tab";

type PageTab = "grants" | "audit";

// Top-level workspace admin page for Persona grants (JEH-1180). Backed by
// the `/api/workspaces/{id}/grants` surface in JEH-1179 — UI is built
// against the proposed shape and parseWithFallback handles drift. Hidden
// when the `cerebro_persona_permissions` flag is off, and when the viewer
// is not a workspace owner/admin (JEH-1217 — admin-gate; backend already
// rejects writes from non-admins, this is defense-in-depth in the UI so a
// non-admin doesn't see a misleading admin surface).
export function PermissionsPage() {
  const enabled = useFeatureFlag("cerebro_persona_permissions");
  const workspace = useCurrentWorkspace();
  const { role, isLoading: isMemberLoading } = useCurrentMember(workspace?.id ?? "");

  const [activeTab, setActiveTab] = useState<PageTab>("grants");
  const [showCreate, setShowCreate] = useState(false);
  const [selectedGrantId, setSelectedGrantId] = useState<string | null>(null);

  if (!enabled) return null;

  if (!workspace || isMemberLoading) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        Workspace context indlæses…
      </div>
    );
  }

  const isAdmin = role === "owner" || role === "admin";
  if (!isAdmin) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-6 text-center">
        <ShieldOff className="size-8 text-muted-foreground" />
        <h2 className="text-base font-medium">Adgang nægtet</h2>
        <p className="max-w-sm text-sm text-muted-foreground">
          Kun workspace-owners og -admins kan administrere Persona grants.
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
          <h1 className="text-sm font-medium">Permissions</h1>
          <p className="ml-2 hidden truncate text-xs text-muted-foreground md:block">
            Workspace grants og policies (Persona)
          </p>
        </div>
      </PageHeader>

      <Tabs
        value={activeTab}
        onValueChange={(v) => setActiveTab(v as PageTab)}
        className="flex flex-1 min-h-0 flex-col"
      >
        <div className="border-b px-4">
          <TabsList variant="line" className="h-10">
            <TabsTrigger value="grants">Grants</TabsTrigger>
            <TabsTrigger value="audit">Audit</TabsTrigger>
          </TabsList>
        </div>

        <TabsContent
          value="grants"
          className="flex flex-1 min-h-0 flex-col"
        >
          <GrantsToolbar
            wsId={wsId}
            onCreate={() => setShowCreate(true)}
          />
          <div className="flex-1 min-h-0 overflow-y-auto">
            <GrantsTable
              wsId={wsId}
              onRowClick={(grantId) => setSelectedGrantId(grantId)}
            />
          </div>
        </TabsContent>

        <TabsContent
          value="audit"
          className="flex flex-1 min-h-0 flex-col"
        >
          <AuditTab wsId={wsId} />
        </TabsContent>
      </Tabs>

      {showCreate && (
        <CreateGrantDialog
          wsId={wsId}
          onClose={() => setShowCreate(false)}
        />
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
