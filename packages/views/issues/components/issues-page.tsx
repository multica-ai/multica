"use client";

import { ListTodo, MessageSquare, Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useIssuesScope } from "@multica/core/issues/stores/issues-scope-store";
import { useStartChatWithDraft } from "../../chat/use-start-chat-with-draft";
import { useT } from "../../i18n";
import { CollectionPageState } from "../../layout/collection-page";
import { IssueSurface } from "../surface/issue-surface";
import { IssueViewHeader } from "../../issue-views/view-header";

const WORKSPACE_BASE = { kind: "workspace" } as const;

export function IssuesPage() {
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
        renderEmpty={({ controller }) => (
          <CollectionPageState
            icon={ListTodo}
            title={t(($) => $.page.empty_title)}
            description={t(($) => $.page.empty_description)}
            actions={
              <>
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
