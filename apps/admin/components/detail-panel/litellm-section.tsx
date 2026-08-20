import { Avatar, AvatarFallback } from "@multica/ui/components/ui/avatar";
import { formatCost } from "@/lib/format";
import type { LiteLlmSection as LiteLlmSectionData } from "@/lib/types";

function initials(name: string): string {
  return name.slice(0, 2).toUpperCase();
}

function formatTokens(value: number | null): string {
  if (value === null) return "—";
  return value.toLocaleString();
}

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
          <div className="grid grid-cols-4 gap-3">
            <div>
              <p className="text-title-lg font-medium text-foreground">{formatCost(litellm.keySpend)}</p>
              <p className="text-caption text-muted-foreground">Key spend</p>
            </div>
            <div>
              <p className="text-title-lg font-medium text-foreground">{formatCost(litellm.cost24h)}</p>
              <p className="text-caption text-muted-foreground">Spend (24h)</p>
            </div>
            <div>
              <p className="text-title-lg font-medium text-foreground">{formatCost(litellm.cost30d)}</p>
              <p className="text-caption text-muted-foreground">Spend (30d)</p>
            </div>
            <div>
              <p className="text-title-lg font-medium text-foreground">{formatTokens(litellm.tokens24h)}</p>
              <p className="text-caption text-muted-foreground">Tokens (24h)</p>
            </div>
          </div>
          <div>
            <dt className="text-label text-muted-foreground">Members</dt>
            {litellm.members.length === 0 ? (
              <p className="mt-0.5 text-body text-muted-foreground">No members reported.</p>
            ) : (
              <ul className="mt-1 flex flex-wrap gap-1.5">
                {litellm.members.map((m) => (
                  <li
                    key={m}
                    className="flex items-center gap-1.5 rounded-full bg-muted py-0.5 pl-0.5 pr-2 text-caption text-foreground"
                  >
                    {/* Plan §5.1 Team Members List: "Avatar + username chips".
                        LiteLLM reports usernames only, no avatar image URL —
                        fall back to initials rather than fetching/guessing one. */}
                    <Avatar size="sm">
                      <AvatarFallback>{initials(m)}</AvatarFallback>
                    </Avatar>
                    {m}
                  </li>
                ))}
              </ul>
            )}
          </div>
        </div>
      )}
    </section>
  );
}
