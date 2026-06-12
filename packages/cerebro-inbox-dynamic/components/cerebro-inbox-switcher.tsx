// TECH-3413 — route-level chooser between the classic inbox and the dynamic
// inbox. Both web and desktop inbox routes render this so the choice (flag +
// per-user mode) is applied in one shared place.
"use client";

import { InboxPage } from "@multica/views/inbox";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { useInboxMode } from "@multica/cerebro-inbox";
import { DynamicInbox } from "./dynamic-inbox";

export function CerebroInboxSwitcher() {
  const dynamicAvailable = useFeatureFlag("cerebro_inbox_dynamic");
  const mode = useInboxMode();
  // TECH-3413 (Jesper feedback #5): the dynamic inbox is a desktop split-view and
  // does not yet have a mobile/PWA layout — on a phone it collapsed badly. Until a
  // dedicated mobile layout ships, mobile always renders the classic inbox, which
  // is already responsive. Desktop honours the user's Classic/Dynamic choice.
  const isMobile = useIsMobile();
  if (!isMobile && dynamicAvailable && mode === "dynamic") return <DynamicInbox />;
  return <InboxPage />;
}
