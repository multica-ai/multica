// TECH-3413 — route-level chooser between the classic inbox and the dynamic
// inbox. Both web and desktop inbox routes render this so the choice (flag +
// per-user mode) is applied in one shared place.
"use client";

import { InboxPage } from "@multica/views/inbox";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useInboxMode } from "@multica/cerebro-inbox";
import { DynamicInbox } from "./dynamic-inbox";

export function CerebroInboxSwitcher() {
  const dynamicAvailable = useFeatureFlag("cerebro_inbox_dynamic");
  const mode = useInboxMode();
  // The user's Classic/Dynamic choice is honoured on every surface. The dynamic
  // inbox now has a single-column mobile layout (DynamicInbox switches on
  // useIsMobile), so the Dynamic toggle works on mobile/PWA too instead of
  // silently falling back to classic.
  if (dynamicAvailable && mode === "dynamic") return <DynamicInbox />;
  return <InboxPage />;
}
