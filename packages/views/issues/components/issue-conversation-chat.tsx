"use client";

import { Virtuoso } from "react-virtuoso";
import { ItemRow, UserMessageContent } from "../../chat/components/chat-message-list";
import { RichContent } from "../../rich-content";
import { CHAT_COLUMN } from "../../chat/components/chat-column";
import { RunOutcomeRow } from "../../common/task-transcript/agent-transcript-dialog";
import type { RunOutcome } from "../../common/task-transcript/run-outcome";
import type { ConversationTimelineItem } from "./issue-conversation-timeline";

/** Presentation only: never invokes the standalone chat controller or starts a run. */
export function IssueConversationChat({ items, isLive, runSummaries }: {
  items: ConversationTimelineItem[]; isLive: boolean;
  runSummaries?: Record<string, { outcome: RunOutcome | null; branch?: string | null }>;
}) {
  return <Virtuoso
    style={{ height: "100%" }}
    data={items}
    initialTopMostItemIndex={{ index: "LAST", align: "end" }}
    followOutput={(atBottom) => atBottom ? "auto" : false}
    computeItemKey={(_, item) => `${item.runId ?? ""}:${item.divider ? "divider" : item.humanId ?? item.seq}`}
    itemContent={(_, item) => {
      if (item.divider) {
        const summary = item.runId ? runSummaries?.[item.runId] : undefined;
        return <>
          <div className={CHAT_COLUMN}><p className="py-2 text-caption text-muted-foreground" role="separator">{item.content}</p></div>
          {summary && <RunOutcomeRow outcome={summary.outcome} branch={summary.branch} />}
        </>;
      }
      return <div className={CHAT_COLUMN}>
        <div className="py-2">
          {item.humanId ? <UserMessageContent content={item.humanContent ?? item.content ?? ""} attachments={[]} />
            : item.type === "text" ? <RichContent content={item.content ?? ""} density="compact" phase={isLive ? "streaming" : "settled"} />
            : <ItemRow item={item} />}
        </div>
      </div>;
    }}
  />;
}
