"use client";

import { ListTodo } from "@multica/ui/icons";
import { useAuthStore } from "@multica/core/auth";
import { myIssuesRelationFromScope } from "@multica/core/issues/stores/my-issues-view-store";
import { PageHeader } from "../../layout/page-header";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { IssueViewHeader } from "../../issue-views/view-header";
import { useT } from "../../i18n";
import { useMyIssuesScope } from "../use-my-issues-scope";

const MY_BASE = { kind: "my" } as const;

export function MyIssuesPage() {
  const { t } = useT("my-issues");
  const user = useAuthStore((s) => s.user);
  const scope = useMyIssuesScope();

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {user ? (
        <IssueSurface
          scope={{
            type: "my",
            userId: user.id,
            relation: myIssuesRelationFromScope(scope),
          }}
          modes={["board", "list", "table", "swimlane"]}
          batchToolbar="list"
          renderHeader={({ controller }) => <IssueViewHeader controller={controller} base={MY_BASE} />}
          renderEmpty={() => (
            <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
              <ListTodo className="h-10 w-10 text-faint-foreground" />
              <p className="text-body">{t(($) => $.page.empty_title)}</p>
              <p className="text-caption">{t(($) => $.page.empty_description)}</p>
            </div>
          )}
        />
      ) : (
        <PageHeader>
          <ListTodo className="size-4 text-muted-foreground" />
          <h1 className="text-body font-medium">{t(($) => $.page.breadcrumb)}</h1>
        </PageHeader>
      )}
    </div>
  );
}
