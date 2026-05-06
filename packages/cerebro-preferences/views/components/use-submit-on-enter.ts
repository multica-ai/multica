// CEREBRO-PATCH(use-submit-on-enter): cerebro modification of upstream file
import { useAuthStore } from "@multica/core/auth";

/**
 * Fork-specific: per-user keybinding preference for message composers.
 *
 * Returns `true` when bare Enter should send a message (Slack-style),
 * `false` when only Cmd/Ctrl+Enter should send (and Enter inserts a newline).
 *
 * Default: `false` (Cmd/Ctrl+Enter sends) — matches the legacy comment-input
 * behaviour. Users opt into Enter-to-send via Settings → Account.
 */
export function useSubmitOnEnter(): boolean {
  const user = useAuthStore((s) => s.user);
  const value = user?.preferences?.submit_on_enter;
  return value === true;
}

export const SUBMIT_ON_ENTER_KEY = "submit_on_enter";
