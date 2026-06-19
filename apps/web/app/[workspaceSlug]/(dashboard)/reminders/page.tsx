"use client";

import { ReminderOverview } from "@multica/cerebro-reminders/views";
import { ErrorBoundary } from "@multica/ui/components/common/error-boundary";

// FIR-394 reminder overview — reminders as their own entity.
export default function Page() {
  return (
    <ErrorBoundary>
      <ReminderOverview />
    </ErrorBoundary>
  );
}
