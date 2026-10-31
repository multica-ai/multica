"use client";

import { ListTodo } from "@multica/ui/icons";
import { useIssuesScope } from "@multica/core/issues/stores/issues-scope-store";
import { useT } from "../../i18n";
import { IssueSurface } from "../surface/issue-surface";
import { IssueViewHeader } from "../../issue-views/view-header";

const WORKSPACE_BASE = { kind: "workspace" } as const;

export function IssuesPage() {
  const { t } = useT("issues");
  const scope = useIssuesScope("issues");

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <IssueSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "table", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller }) => <IssueViewHeader controller={controller} base={WORKSPACE_BASE} />}
        renderEmpty={() => (
          <div className="flex flex-1 min-h-0 flex-col items-center justify-center gap-2 text-muted-foreground">
            <ListTodo className="h-10 w-10 text-faint-foreground" />
            <p className="text-body">{t(($) => $.page.empty_title)}</p>
          </div>
        )}
      />
    </div>
  );
}
