"use client";

import { TranscriptButton } from "../../common/task-transcript/transcript-button";
import type { AgentTask } from "@multica/core/types/agent";

/**
 * Inline "View run" chip that opens the same transcript dialog the sidebar
 * uses. Pure presentational wrapper over {@link TranscriptButton}: the parent
 * has already resolved the AgentTask + agentName and decided the button
 * should render (the comment card gates on source_task_id lookup result).
 *
 * TranscriptButton is an icon-only trigger by design; the "View run"
 * semantics live in its tooltip / aria-label (passed as the `title` prop).
 * Alignment with the retry button (which sits in the same slot) is handled
 * via the shared `className` prop, set to `mt-2 pl-10` (root) or
 * `mt-2 pl-12 pr-4` (reply) by the caller — identical to TaskCommentRetryButton.
 *
 * Inherits accessibility (aria-label, tooltip, loading state, dialog live
 * region) from the shared button.
 */
export function ViewRunButton({
  agentTask,
  agentName,
  title,
  className,
}: {
  agentTask: AgentTask;
  agentName: string;
  title: string;
  className?: string;
}) {
  return (
    <TranscriptButton
      task={agentTask}
      agentName={agentName}
      title={title}
      className={className}
    />
  );
}