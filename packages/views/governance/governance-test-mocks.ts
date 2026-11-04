// Shared fixtures for the governance view tests. Test-only; nothing ships from here.
import type { CompanyStateInfo, GovernanceEvent } from "@multica/core/governance";

export const RUNNING: CompanyStateInfo = {
  state: "running",
  setting: "running",
  quiet_hours: { enabled: false, start: "22:00", end: "07:00", timezone: "UTC" },
  updated_at: "2026-11-02T10:00:00Z",
};

export function withState(state: CompanyStateInfo["state"], extra: Partial<CompanyStateInfo> = {}): CompanyStateInfo {
  return { ...RUNNING, state, setting: state, ...extra };
}

export function event(id: string, extra: Partial<GovernanceEvent> = {}): GovernanceEvent {
  return {
    id,
    kind: "budget_hold",
    actor_type: "system",
    actor_id: null,
    subject_type: "budget",
    subject_id: "bud-1",
    summary: `Summary ${id}`,
    details: {},
    created_at: "2026-11-02T10:00:00Z",
    ...extra,
  };
}
