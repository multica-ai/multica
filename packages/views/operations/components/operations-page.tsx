"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentToolApprovalListOptions,
  operationalCapabilityListOptions,
  operationalControlKeys,
  operationalSummaryOptions,
} from "@multica/core/operational-controls";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { ApprovalQueue } from "./approval-queue";

export function OperationsPage() {
  const { t } = useT("operations");
  const workspaceId = useWorkspaceId();
  const queryClient = useQueryClient();
  const params = { days: 7, tz: Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC" };
  const summary = useQuery(operationalSummaryOptions(workspaceId, params));
  const capabilities = useQuery(operationalCapabilityListOptions(workspaceId));
  const approvals = useQuery(agentToolApprovalListOptions(workspaceId, { status: "pending", limit: 100 }));
  const decision = useMutation({
    mutationFn: ({ id, decision, reasonCode }: { id: string; decision: "approve" | "deny"; reasonCode: string }) =>
      api.decideAgentToolApproval(id, { decision, reason_code: reasonCode, expected_status: "pending" }),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: operationalControlKeys.all(workspaceId) }),
  });

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-body font-semibold">{t(($) => $.title)}</h1>
          <p className="truncate text-caption text-muted-foreground">{t(($) => $.description)}</p>
        </div>
      </PageHeader>
      <div className="mx-auto w-full max-w-[1440px] space-y-6 p-4 sm:p-6">
        {summary.data && (
          <section className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Metric label={t(($) => $.summary.intercepted)} value={summary.data.intercepted_invocation_count} />
            <Metric label={t(($) => $.summary.pending)} value={summary.data.pending} />
            <Metric label={t(($) => $.summary.failed)} value={summary.data.failed} />
            <Metric label={t(($) => $.summary.gaps)} value={t(($) => $.summary.gap_count, { count: summary.data.configured_agent_capability_gaps })} />
          </section>
        )}

        <section className="rounded-xl border p-4 sm:p-6">
          <h2 className="text-title-sm font-medium">{t(($) => $.approvals.title)}</h2>
          <div className="mt-4">
            <ApprovalQueue
              approvals={approvals.data?.items ?? []}
              decidingId={decision.isPending ? decision.variables?.id : undefined}
              onDecision={async (id, nextDecision, reasonCode) => {
                await decision.mutateAsync({ id, decision: nextDecision, reasonCode });
              }}
            />
          </div>
        </section>

        <section className="rounded-xl border p-4 sm:p-6">
          <h2 className="text-title-sm font-medium">{t(($) => $.capabilities.title)}</h2>
          <div className="mt-4 space-y-3">
            {(capabilities.data?.capabilities ?? []).map((capability) => (
              <div key={`${capability.transport_kind}:${capability.provider_family}:${capability.name}`} className="flex flex-col gap-1 rounded-lg border p-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <p className="text-body font-medium">{capability.name}</p>
                  <p className="text-caption text-muted-foreground">{capability.transport_kind} · {capability.provider_family}</p>
                </div>
                <p className="text-caption text-muted-foreground">
                  {capability.supported ? t(($) => $.capabilities.available) : capability.offline_reason ?? t(($) => $.capabilities.unavailable)}
                </p>
              </div>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string | number }) {
  return <div className="rounded-xl border p-4"><p className="text-caption text-muted-foreground">{label}</p><p className="mt-2 text-title-lg font-semibold">{value}</p></div>;
}
