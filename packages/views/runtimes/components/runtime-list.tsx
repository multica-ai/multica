import { useState, useEffect } from "react";
import { Server, ArrowUpCircle, ChevronDown, Check, Copy } from "lucide-react";
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
import { ProviderLogo } from "./provider-logo";

type RuntimeFilter = "mine" | "all";
type OS = "windows" | "mac" | "linux";

function CopyableCode({ code }: { code: string }) {
  const [copied, setCopied] = useState(false);

  const copy = () => {
    navigator.clipboard?.writeText(code);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <div className="flex items-start gap-2 rounded-md bg-muted px-3 py-2">
      <pre className="flex-1 overflow-x-auto font-mono text-xs leading-relaxed whitespace-pre">{code}</pre>
      <button
        onClick={copy}
        className="shrink-0 mt-0.5 text-muted-foreground hover:text-foreground transition-colors"
        aria-label="Copy"
      >
        {copied ? <Check className="h-3.5 w-3.5 text-success" /> : <Copy className="h-3.5 w-3.5" />}
      </button>
    </div>
  );
}

function DaemonSetupGuide() {
  const [os, setOs] = useState<OS>("linux");
  const [baseUrl, setBaseUrl] = useState("");

  useEffect(() => {
    if (typeof window !== "undefined") setBaseUrl(window.location.origin);
    if (typeof navigator === "undefined") return;
    const p = navigator.platform || "";
    const ua = navigator.userAgent || "";
    if (/Win/i.test(p) || /Windows/i.test(ua)) setOs("windows");
    else if (/Mac|iPhone|iPad|iPod/i.test(p) || /Mac OS X/i.test(ua)) setOs("mac");
    else setOs("linux");
  }, []);

  const url = baseUrl;
  const sep = os === "windows" ? "; " : " && ";

  const installCmd =
    os === "windows"
      ? `$d="$env:USERPROFILE\\.local\\bin"; New-Item -Force -ItemType Directory $d | Out-Null; Invoke-WebRequest -Uri "${url}/dist/windows/multica.exe" -OutFile "$d\\multica.exe"; [Environment]::SetEnvironmentVariable("PATH","$([Environment]::GetEnvironmentVariable('PATH','User'));$d","User"); $env:PATH+=";$d"`
      : os === "mac"
      ? `mkdir -p $HOME/.local/bin${sep}curl -fsSL ${url}/dist/darwin/multica -o $HOME/.local/bin/multica${sep}chmod +x $HOME/.local/bin/multica${sep}echo 'export PATH="$PATH:$HOME/.local/bin"' >> $HOME/.zshrc${sep}export PATH="$PATH:$HOME/.local/bin"`
      : `mkdir -p $HOME/.local/bin${sep}curl -fsSL ${url}/dist/linux/multica -o $HOME/.local/bin/multica${sep}chmod +x $HOME/.local/bin/multica${sep}echo 'export PATH="$PATH:$HOME/.local/bin"' >> $HOME/.bashrc${sep}export PATH="$PATH:$HOME/.local/bin"`;

  const configCmd = `multica config set server_url ${url}${sep}multica config set app_url ${url}`;

  const steps: { title: string; code: string; note?: string }[] = [
    { title: "Install", code: installCmd, note: "Downloads binary and adds to PATH" },
    { title: "Configure server URL", code: configCmd },
    { title: "Login", code: "multica login", note: "Opens browser for authentication" },
    { title: "Start daemon", code: "multica daemon start" },
  ];

  const osLabels: { key: OS; label: string }[] = [
    { key: "mac", label: "macOS" },
    { key: "linux", label: "Linux" },
    { key: "windows", label: "Windows" },
  ];

  return (
    <div className="px-4 py-6 space-y-4">
      <div className="flex flex-col items-center text-center gap-1">
        <Server className="h-8 w-8 text-muted-foreground/40" />
        <p className="mt-2 text-sm font-medium">No runtimes registered</p>
        <p className="text-xs text-muted-foreground">
          Follow the steps below to connect this machine as a runtime.
        </p>
      </div>

      <div className="flex items-center justify-center gap-0.5 rounded-md bg-muted p-0.5 w-fit mx-auto">
        {osLabels.map(({ key, label }) => (
          <button
            key={key}
            onClick={() => setOs(key)}
            className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
              os === key
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {label}
          </button>
        ))}
      </div>

      <div className="space-y-3">
        {steps.map((step, i) => (
          <div key={i} className="space-y-1.5">
            <div className="flex items-center gap-2">
              <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground">
                {i + 1}
              </span>
              <span className="text-xs font-medium">{step.title}</span>
            </div>
            <CopyableCode code={step.code} />
            {step.note && (
              <p className="text-xs text-muted-foreground pl-7">{step.note}</p>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

function RuntimeListItem({
  runtime,
  isSelected,
  ownerMember,
  hasUpdate,
  onClick,
}: {
  runtime: AgentRuntime;
  isSelected: boolean;
  ownerMember: MemberWithUser | null;
  hasUpdate: boolean;
  onClick: () => void;
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
          <span title="Update available">
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
}: {
  runtimes: AgentRuntime[];
  selectedId: string;
  onSelect: (id: string) => void;
  filter: RuntimeFilter;
  onFilterChange: (filter: RuntimeFilter) => void;
  ownerFilter: string | null;
  onOwnerFilterChange: (ownerId: string | null) => void;
  updatableIds?: Set<string>;
}) {
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
        <h1 className="text-sm font-semibold">Runtimes</h1>
        <span className="text-xs text-muted-foreground">
          {filteredRuntimes.filter((r) => r.status === "online").length}/
          {filteredRuntimes.length} online
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
            Mine
          </button>
          <button
            onClick={() => { onFilterChange("all"); onOwnerFilterChange(null); }}
            className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
              filter === "all"
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            All
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
                <span>Owner</span>
              )}
              <ChevronDown className="h-3 w-3 opacity-50" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem
                onClick={() => onOwnerFilterChange(null)}
                className="flex items-center justify-between"
              >
                <span className="text-xs">All owners</span>
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
        filter === "mine" ? (
          <DaemonSetupGuide />
        ) : (
          <div className="flex flex-col items-center justify-center px-4 py-12">
            <Server className="h-8 w-8 text-muted-foreground/40" />
            <p className="mt-3 text-sm text-muted-foreground">
              {ownerFilter ? "No runtimes for this owner" : "No runtimes registered"}
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
            />
          ))}
        </div>
      )}
    </div>
  );
}
