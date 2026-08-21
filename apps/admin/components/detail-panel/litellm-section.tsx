import { formatCost } from "@/lib/format";
import type { LiteLlmSection as LiteLlmSectionData } from "@/lib/types";

export function LiteLlmSection({ litellm }: { litellm: LiteLlmSectionData }) {
  return (
    <section>
      <h3 className="mb-3 text-label font-medium text-muted-foreground uppercase tracking-wide">
        LiteLLM
      </h3>
      {!litellm.linked ? (
        <p className="text-body text-muted-foreground">No LiteLLM key linked to this workspace.</p>
      ) : (
        <div className="space-y-4">
          <dl className="grid grid-cols-2 gap-4">
            <div>
              <dt className="text-label text-muted-foreground">Key alias</dt>
              <dd className="mt-0.5 text-body text-foreground">{litellm.keyAlias ?? "—"}</dd>
            </div>
            <div>
              <dt className="text-label text-muted-foreground">Team</dt>
              <dd className="mt-0.5 text-body text-foreground">{litellm.teamAlias ?? "—"}</dd>
            </div>
          </dl>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <p className="text-title-lg font-medium text-foreground">{formatCost(litellm.keySpend)}</p>
              <p className="text-caption text-muted-foreground">Key spend</p>
            </div>
            <div>
              <p className="text-title-lg font-medium text-foreground">{formatCost(litellm.costPerTicket)}</p>
              <p className="text-caption text-muted-foreground">Cost / active ticket</p>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
