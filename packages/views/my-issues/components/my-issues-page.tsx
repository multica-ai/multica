"use client";

import { CircleUser, ListTodo, Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useAuthStore } from "@multica/core/auth";
import { myIssuesRelationFromScope } from "@multica/core/issues/stores/my-issues-view-store";
import { CollectionPageState } from "../../layout/collection-page";
import { PageHeader } from "../../layout/page-header";
import { IssueSurface } from "../../issues/surface/issue-surface";
import { IssueViewHeader } from "../../issue-views/view-header";
import { useT } from "../../i18n";
import { useMyIssuesScope } from "../use-my-issues-scope";

const MY_BASE = { kind: "my" } as const;

export function MyIssuesPage() {
  const { t } = useT("my-issues");
  const { t: tIssues } = useT("issues");
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
          renderEmpty={({ controller }) => (
            <CollectionPageState
              icon={CircleUser}
              title={t(($) => $.page.empty_title)}
              description={t(($) => $.page.empty_description)}
              actions={
                <Button size="sm" onClick={() => controller.openCreateIssue()}>
                  <Plus aria-hidden="true" />
                  {tIssues(($) => $.views.new_issue)}
                </Button>
              }
            />
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
