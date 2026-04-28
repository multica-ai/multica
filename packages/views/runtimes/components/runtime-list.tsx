import { Server, ArrowUpCircle, ChevronDown, Check, Loader2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar } from "../../common/actor-avatar";
import { PageHeader } from "../../layout/page-header";
import { useRuntimesT, type RuntimesDict } from "../i18n";
import { ProviderLogo } from "./provider-logo";

type RuntimeFilter = "mine" | "all";

function RuntimeListItem({
  runtime,
  isSelected,
  ownerMember,
  hasUpdate,
  onClick,
  t,
}: {
  runtime: AgentRuntime;
  isSelected: boolean;
  ownerMember: MemberWithUser | null;
  hasUpdate: boolean;
  onClick: () => void;
  t: RuntimesDict;
}) {
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <div className="flex h-8 w-8 shrink-0 items-center justify-center">
        <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{runtime.name}</div>
        <div className="mt-0.5 flex items-center gap-1 text-xs text-muted-foreground">
          {ownerMember ? (
            <>
              <ActorAvatar
                actorType="member"
                actorId={ownerMember.user_id}
                size={14}
              />
              <span className="truncate">{ownerMember.name}</span>
            </>
          ) : (
            <span className="truncate">{runtime.runtime_mode}</span>
          )}
        </div>
      </div>
      <div className="flex items-center gap-1.5 shrink-0">
        {hasUpdate && (
          <span title={t.list.updateAvailable}>
            <ArrowUpCircle className="h-3.5 w-3.5 text-info" />
          </span>
        )}
        <div
          className={`h-2 w-2 rounded-full ${
            runtime.status === "online" ? "bg-success" : "bg-muted-foreground/40"
          }`}
        />
      </div>
    </button>
  );
}

export function RuntimeList({
  runtimes,
  selectedId,
  onSelect,
  filter,
  onFilterChange,
  ownerFilter,
  onOwnerFilterChange,
  updatableIds,
  bootstrapping,
}: {
  runtimes: AgentRuntime[];
  selectedId: string;
  onSelect: (id: string) => void;
  filter: RuntimeFilter;
  onFilterChange: (filter: RuntimeFilter) => void;
  ownerFilter: string | null;
  onOwnerFilterChange: (ownerId: string | null) => void;
  updatableIds?: Set<string>;
  /**
   * When true and no runtimes are visible, the empty state renders a
   * "starting" indicator instead of the static "register a runtime"
   * hint. The desktop shell sets this while its bundled daemon is
   * still booting / registering — without the hint, users see a
   * misleading "no runtimes" message during the few seconds between
   * page load and daemon registration. Web leaves this undefined.
   */
  bootstrapping?: boolean;
}) {
  const t = useRuntimesT();
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const getOwnerMember = (ownerId: string | null) => {
    if (!ownerId) return null;
    return members.find((m) => m.user_id === ownerId) ?? null;
  };

  // Get unique owners from runtimes for filter dropdown
  const uniqueOwners = filter === "all"
    ? Array.from(new Set(runtimes.map((r) => r.owner_id).filter(Boolean) as string[]))
        .map((id) => members.find((m) => m.user_id === id))
        .filter(Boolean) as MemberWithUser[]
    : [];

  // Count runtimes per owner
  const ownerCounts = new Map<string, number>();
  for (const r of runtimes) {
    if (r.owner_id) ownerCounts.set(r.owner_id, (ownerCounts.get(r.owner_id) ?? 0) + 1);
  }

  // Apply client-side owner filter when in "all" mode
  const filteredRuntimes = filter === "all" && ownerFilter
    ? runtimes.filter((r) => r.owner_id === ownerFilter)
    : runtimes;

  const selectedOwner = ownerFilter ? getOwnerMember(ownerFilter) : null;

  return (
    <div className="overflow-y-auto h-full border-r">
      <PageHeader className="justify-between">
        <h1 className="text-sm font-semibold">{t.page.title}</h1>
        <span className="text-xs text-muted-foreground">
          {t.page.onlineCount(
            filteredRuntimes.filter((r) => r.status === "online").length,
            filteredRuntimes.length,
          )}
        </span>
      </PageHeader>

      {/* Filter bar */}
      <div className="flex items-center justify-between border-b px-4 py-2">
        {/* Scope toggle */}
        <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
          <button
            onClick={() => { onFilterChange("mine"); onOwnerFilterChange(null); }}
            className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
              filter === "mine"
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {t.list.filterMine}
          </button>
          <button
            onClick={() => { onFilterChange("all"); onOwnerFilterChange(null); }}
            className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
              filter === "all"
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {t.list.filterAll}
          </button>
        </div>

        {/* Owner dropdown (only in All mode with multiple owners) */}
        {filter === "all" && uniqueOwners.length > 1 && (
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <button className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground hover:bg-accent" />
              }
            >
              {selectedOwner ? (
                <>
                  <ActorAvatar actorType="member" actorId={selectedOwner.user_id} size={16} />
                  <span className="max-w-20 truncate">{selectedOwner.name}</span>
                </>
              ) : (
                <span>{t.list.ownerLabel}</span>
              )}
              <ChevronDown className="h-3 w-3 opacity-50" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem
                onClick={() => onOwnerFilterChange(null)}
                className="flex items-center justify-between"
              >
                <span className="text-xs">{t.list.allOwners}</span>
                {!ownerFilter && <Check className="h-3.5 w-3.5 text-foreground" />}
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              {uniqueOwners.map((m) => (
                <DropdownMenuItem
                  key={m.user_id}
                  onClick={() => onOwnerFilterChange(ownerFilter === m.user_id ? null : m.user_id)}
                  className="flex items-center justify-between"
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
                    <span className="text-xs truncate">{m.name}</span>
                    <span className="text-xs text-muted-foreground">{ownerCounts.get(m.user_id) ?? 0}</span>
                  </div>
                  {ownerFilter === m.user_id && <Check className="h-3.5 w-3.5 shrink-0 text-foreground" />}
                </DropdownMenuItem>
              ))}
            </DropdownMenuContent>
          </DropdownMenu>
        )}
      </div>

      {filteredRuntimes.length === 0 ? (
        bootstrapping ? (
          <div className="flex flex-col items-center justify-center px-4 py-12">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground/60" />
            <p className="mt-3 text-sm text-muted-foreground">
              {t.list.bootstrapping}
            </p>
            <p className="mt-1 text-xs text-muted-foreground text-center">
              {t.list.bootstrappingHint}
            </p>
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center px-4 py-12">
            <Server className="h-8 w-8 text-muted-foreground/40" />
            <p className="mt-3 text-sm text-muted-foreground">
              {filter === "mine" ? t.list.emptyMine : ownerFilter ? t.list.emptyAllOwner : t.list.emptyAll}
            </p>
            <p className="mt-1 text-xs text-muted-foreground text-center">
              {t.list.emptyHintBefore}
              <code className="rounded bg-muted px-1 py-0.5">
                multica daemon start
              </code>
              {t.list.emptyHintAfter}
            </p>
          </div>
        )
      ) : (
        <div className="divide-y">
          {filteredRuntimes.map((runtime) => (
            <RuntimeListItem
              key={runtime.id}
              runtime={runtime}
              isSelected={runtime.id === selectedId}
              ownerMember={getOwnerMember(runtime.owner_id)}
              hasUpdate={updatableIds?.has(runtime.id) ?? false}
              onClick={() => onSelect(runtime.id)}
              t={t}
            />
          ))}
        </div>
      )}
    </div>
  );
}
