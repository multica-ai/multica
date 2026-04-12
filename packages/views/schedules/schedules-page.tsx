"use client";

import { SchedulesTab } from "../settings/components/schedules-tab";

export function SchedulesPage() {
  return (
    <div className="flex-1 overflow-y-auto">
      <div className="w-full max-w-4xl mx-auto p-6">
        <SchedulesTab />
      </div>
    </div>
  );
}
