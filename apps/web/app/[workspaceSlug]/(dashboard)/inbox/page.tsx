"use client";

import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { InboxPage as UpstreamInboxPage } from "@multica/views/inbox";
import { CerebroInboxPage } from "@multica/cerebro-inbox";

export default function InboxRoute() {
  const useCerebro = useFeatureFlag("cerebro_inbox");
  return useCerebro ? <CerebroInboxPage /> : <UpstreamInboxPage />;
}
