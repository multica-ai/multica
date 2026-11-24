"use client";

import { ListTodo, MessageSquare, Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueViewContainerKey, useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { useIssuesScope } from "@multica/core/issues/stores/issues-scope-store";
import { useStartChatWithDraft } from "../../chat/use-start-chat-with-draft";
import { useT } from "../../i18n";
import { CollectionPageState } from "../../layout/collection-page";
import { IssueSurface } from "../surface/issue-surface";
import { IssueViewHeader } from "../../issue-views/view-header";
import { SNOOZED_VIEW_ID } from "../../issue-views/builtin-views";
import { SnoozedInboxPage } from "../../inbox/components/snoozed-inbox-page";

const WORKSPACE_BASE = { kind: "workspace" } as const;
const WORKSPACE_SCOPE = { scope_type: "workspace" } as const;

export function IssuesPage() {
  const wsId = useWorkspaceId();
  const activeViewId = useActiveIssueViewStore((s) => s.active[issueViewContainerKey(wsId, WORKSPACE_SCOPE)] ?? null);
  // Snoozed is the Inbox's, listed by when each issue comes back, not a filter of the table.
  if (activeViewId === SNOOZED_VIEW_ID) return <SnoozedInboxPage />;
  return <IssuesSurfacePage />;
}

function IssuesSurfacePage() {
  const { t } = useT("issues");
  const scope = useIssuesScope("issues");
  const startChat = useStartChatWithDraft();

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <IssueSurface
        scope={{ type: "workspace", actorKind: scope }}
        modes={["board", "list", "table", "swimlane"]}
        batchToolbar="list"
        renderHeader={({ controller }) => <IssueViewHeader controller={controller} base={WORKSPACE_BASE} />}
        renderEmpty={({ controller, canCreate }) => (
          <CollectionPageState
            icon={ListTodo}
            title={t(($) => $.page.empty_title)}
            description={t(($) => $.page.empty_description)}
            actions={
              canCreate && <>
                <Button size="sm" onClick={() => controller.openCreateIssue()}>
                  <Plus aria-hidden="true" />
                  {t(($) => $.views.new_issue)}
                </Button>
                {startChat ? (
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => startChat(t(($) => $.page.break_down_prompt))}
                  >
                    <MessageSquare aria-hidden="true" />
                    {t(($) => $.page.break_down_with_chat)}
                  </Button>
                ) : null}
              </>
            }
          />
        )}
      />
    </div>
  );
}
