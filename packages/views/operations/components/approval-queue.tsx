import type { AgentToolApproval } from "@multica/core/operational-controls";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

export function ApprovalQueue({
  approvals,
  onDecision,
  decidingId,
}: {
  approvals: AgentToolApproval[];
  onDecision: (
    id: string,
    decision: "approve" | "deny",
    reasonCode: string,
  ) => Promise<void>;
  decidingId?: string;
}) {
  const { t } = useT("operations");
  if (approvals.length === 0) {
    return <p className="text-body text-muted-foreground">{t(($) => $.approvals.empty)}</p>;
  }

  return (
    <div className="space-y-3">
      {approvals.map((approval) => (
        <article key={approval.id} className="rounded-lg border p-4">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
            <div className="min-w-0 space-y-1">
              <p className="truncate text-body font-medium">
                {approval.server_key}:{approval.tool_name}
              </p>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.approvals.fields)}: {approval.schema_field_names.join(", ")}
              </p>
              <p className="text-caption text-muted-foreground">
                {t(($) => $.approvals.metadata_warning)}
              </p>
            </div>
            <div className="flex shrink-0 gap-2">
              <Button
                size="sm"
                disabled={decidingId === approval.id}
                onClick={() => void onDecision(approval.id, "approve", "human_approved")}
              >
                {t(($) => $.approvals.approve)}
              </Button>
              <Button
                size="sm"
                variant="outline"
                disabled={decidingId === approval.id}
                onClick={() => void onDecision(approval.id, "deny", "human_denied")}
              >
                {t(($) => $.approvals.deny)}
              </Button>
            </div>
          </div>
        </article>
      ))}
    </div>
  );
}
