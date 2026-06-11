"use client";

// TECH-3413 — the inbox route renders the cerebro switcher, which picks the
// classic or dynamic inbox per the cerebro_inbox_dynamic flag + the user's mode.
import { CerebroInboxSwitcher } from "@multica/cerebro-inbox-dynamic";

export default function InboxRoute() {
  return <CerebroInboxSwitcher />;
}
