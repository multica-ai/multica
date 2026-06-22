"use client";

import type { AgentTask } from "@multica/core/types/agent";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { sessionForTime } from "./grouping";
import { useSessions } from "./use-sessions";

// FIR-1787 point 3: the Agent Runs run-log (Execution history) used to show only
// a timestamp, duration and status — none of what was agreed. This subtitle adds
// the two things a reader needs to tell runs apart: the initial prompt that
// kicked the run off (trigger_summary, the verbatim snapshot, falling back to the
// curated title) and the name of the session the run belongs to (resolved from
// the run's created_at against the issue's session markers).
//
// Render-only and gated by the same cerebro_comment_chapters flag as the rest of
// the sessions UI: when the flag is off this renders nothing and the run-log is
// byte-for-byte unchanged. useSessions shares its query key with issue-detail, so
// no extra network request is made for an issue already on screen.
export function RunLogPromptLine({ task }: { task: AgentTask }) {
  const enabled = useFeatureFlag("cerebro_comment_chapters");
  const { data: sessions } = useSessions(task.issue_id);

  if (!enabled) return null;

  const prompt = (task.trigger_summary || task.title || "").trim();
  const session = task.issue_id ? sessionForTime(sessions, task.created_at) : null;
  if (!prompt && !session) return null;

  return (
    <div className="ml-5 mt-0.5 flex min-w-0 items-baseline gap-1.5 text-[11px] text-muted-foreground">
      {session && (
        <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 font-medium text-foreground/70">
          {session.name}
        </span>
      )}
      {prompt && (
        <span className="truncate" title={prompt}>
          {prompt}
        </span>
      )}
    </div>
  );
}
