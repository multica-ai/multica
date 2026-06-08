"use client";

// CEREBRO-PATCH(cerebro-dashboard-spend-table): TECH-3093 spend per person table
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { useDashboardStore } from "../../core/store";
import type { DashboardOverview } from "../../core/api";

function usdFmt(cents: number): string {
  if (!cents) return "—";
  return `$${(cents / 100).toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`;
}

interface Props {
  data: DashboardOverview | undefined;
  isLoading: boolean;
}

export function MessageSpendTable({ data, isLoading }: Props) {
  const openMessagePanel = useDashboardStore((s) => s.openMessagePanel);
  const senders = (data?.top_message_senders ?? []).filter(
    (s) => s.spend_cents !== undefined && s.spend_cents > 0,
  );

  return (
    <div className="rounded-lg border bg-card">
      <div className="border-b px-4 py-2.5">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          Cost per person
        </h3>
      </div>

      {isLoading ? (
        <div className="space-y-px p-2">
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-8 animate-pulse rounded bg-muted" />
          ))}
        </div>
      ) : senders.length === 0 ? (
        <p className="py-6 text-center text-xs text-muted-foreground">
          No spend data for this period.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="border-b text-left text-[11px] text-muted-foreground">
                <th className="px-3 py-2 font-medium">Person</th>
                <th className="px-3 py-2 text-right font-medium">Messages</th>
                <th className="px-3 py-2 text-right font-medium">Cost (USD)</th>
              </tr>
            </thead>
            <tbody>
              {senders.map((actor) => (
                <tr
                  key={actor.id}
                  className="border-b last:border-0 transition-colors hover:bg-accent/30"
                >
                  <td className="px-3 py-2">
                    <button
                      type="button"
                      className="flex items-center gap-2 font-medium hover:underline"
                      onClick={() => openMessagePanel(actor.id, actor.name || "Unknown")}
                    >
                      <ActorAvatar
                        name={actor.name || "?"}
                        initials={(actor.name || "?").charAt(0).toUpperCase()}
                        size={18}
                      />
                      {actor.name || "Unknown"}
                    </button>
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums text-muted-foreground">
                    {actor.count.toLocaleString()}
                  </td>
                  <td className="px-3 py-2 text-right tabular-nums font-medium">
                    {usdFmt(actor.spend_cents ?? 0)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
