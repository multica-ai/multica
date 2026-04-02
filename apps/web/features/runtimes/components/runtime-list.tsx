import Link from "next/link";
import { Server } from "lucide-react";
import type { AgentRuntime } from "@/shared/types";
import { RuntimeModeIcon } from "./shared";

function RuntimeListItem({
  runtime,
  isSelected,
  onClick,
}: {
  runtime: AgentRuntime;
  isSelected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-3 px-4 py-3 text-left transition-colors ${
        isSelected ? "bg-accent" : "hover:bg-accent/50"
      }`}
    >
      <div
        className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-lg ${
          runtime.status === "online" ? "bg-success/10" : "bg-muted"
        }`}
      >
        <RuntimeModeIcon mode={runtime.runtime_mode} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="truncate text-sm font-medium">{runtime.name}</div>
        <div className="mt-0.5 truncate text-xs text-muted-foreground">
          {runtime.provider} &middot; {runtime.runtime_mode}
        </div>
      </div>
      <div
        className={`h-2 w-2 shrink-0 rounded-full ${
          runtime.status === "online" ? "bg-success" : "bg-muted-foreground/40"
        }`}
      />
    </button>
  );
}

export function RuntimeList({
  runtimes,
  selectedId,
  onSelect,
}: {
  runtimes: AgentRuntime[];
  selectedId: string;
  onSelect: (id: string) => void;
}) {
  return (
    <div className="overflow-y-auto h-full border-r">
      <div className="flex h-12 items-center justify-between border-b px-4">
        <h1 className="text-sm font-semibold">Runtimes</h1>
        <span className="text-xs text-muted-foreground">
          {runtimes.filter((r) => r.status === "online").length}/
          {runtimes.length} online
        </span>
      </div>
      {runtimes.length === 0 ? (
        <div className="flex flex-col items-center justify-center px-4 py-12">
          <Server className="h-8 w-8 text-muted-foreground/40" />
          <p className="mt-3 text-sm text-muted-foreground">
            No runtimes registered
          </p>
          <p className="mt-2 max-w-sm text-xs text-muted-foreground text-center leading-relaxed">
            The web app and the CLI use separate credentials. After signing in here, connect the CLI once:
            open{" "}
            <Link
              href="/settings?tab=tokens"
              className="text-primary underline-offset-2 hover:underline"
            >
              Settings → API Tokens
            </Link>
            , create a token, then run{" "}
            <code className="rounded bg-muted px-1 py-0.5">multica login --token</code>{" "}
            and paste it (this also watches your workspaces). Finally run{" "}
            <code className="rounded bg-muted px-1 py-0.5">multica daemon start</code>.
          </p>
          <p className="mt-3 text-xs text-muted-foreground text-center">
            Or run{" "}
            <code className="rounded bg-muted px-1 py-0.5">multica login</code>{" "}
            and finish in the browser (click Authorize if you are already signed in).
          </p>
        </div>
      ) : (
        <div className="divide-y">
          {runtimes.map((runtime) => (
            <RuntimeListItem
              key={runtime.id}
              runtime={runtime}
              isSelected={runtime.id === selectedId}
              onClick={() => onSelect(runtime.id)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
