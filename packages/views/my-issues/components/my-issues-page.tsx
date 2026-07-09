"use client";

import { useStore } from "zustand";
import { ListTodo } from "lucide-react";
import { useAuthStore } from "@multica/core/auth";
import {
  myIssuesViewStore,
  type MyIssuesScope,
} from "@multica/core/issues/stores/my-issues-view-store";
import { PageHeader } from "../../layout/page-header";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { useT } from "../../i18n";
import { MyIssuesHeader } from "./my-issues-header";

function relationFromScope(scope: MyIssuesScope) {
  return scope === "agents" ? "involved" : scope;
}

export function MyIssuesPage() {
  const { t } = useT("my-issues");
  const user = useAuthStore((s) => s.user);
  const scope = useStore(myIssuesViewStore, (s) => s.scope);
  const setScope = useStore(myIssuesViewStore, (s) => s.setScope);

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="gap-2">
        <ListTodo className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.breadcrumb)}</h1>
      </PageHeader>

      {user ? (
        <IssueSurface
          scope={{
            type: "my",
            userId: user.id,
            relation: relationFromScope(scope),
          }}
          modes={["board", "list", "swimlane"]}
          batchToolbar="list"
          renderHeader={({ controller }) => (
            <MyIssuesHeader
              allIssues={controller.surfaceIssues}
              scope={scope}
              onScopeChange={setScope}
              isRefreshing={controller.isRefreshing}
            />
          )}
          renderEmpty={() => (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-muted-foreground/40" />
              <p className="text-sm">{t(($) => $.page.empty_title)}</p>
              <p className="text-xs">{t(($) => $.page.empty_description)}</p>
            </div>
          )}
        />
      ) : null}
    </div>
  );
}
