"use client";

import type { ReactNode } from "react";
import { Laptop, Monitor, Server } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { GroupBand } from "./feature-visuals";
import { FLOATING, PROVIDER_MARKS, ZoomCanvas } from "./mock-kit";

/*
 * Mockups for the Multica CLI page: a terminal connecting a computer with
 * `multica setup`, the Runtimes list it lands in, and a coding agent driving
 * the CLI on someone's behalf. Command output follows the CLI's own strings;
 * data is fictional.
 */

export function TerminalWindow({ title, className, children }: { title: string; className?: string; children: ReactNode }) {
  return (
    <div className={cn("flex flex-col overflow-hidden rounded-xl border border-site-line-strong bg-site-surface shadow-site-float", className)}>
      <div className="flex h-9 shrink-0 items-center gap-1.5 border-b border-site-line bg-site-sunken px-3">
        {[0, 1, 2].map((dot) => (
          <span key={dot} className="size-2.5 shrink-0 rounded-full bg-site-line-strong" />
        ))}
        <span className="mx-auto pr-10 text-caption text-muted-foreground">{title}</span>
      </div>
      <div className="flex min-h-0 flex-1 flex-col gap-1 overflow-hidden px-4 py-3.5 font-mono text-[12px] leading-[1.6] text-muted-foreground *:shrink-0 *:whitespace-pre">
        {children}
      </div>
    </div>
  );
}

export function Cmd({ children }: { children: ReactNode }) {
  return (
    <p className="text-foreground">
      <span className="text-muted-foreground select-none">$ </span>
      {children}
    </p>
  );
}

function Gap() {
  return <span aria-hidden className="h-2" />;
}

function Cursor() {
  return (
    <p className="text-foreground">
      <span className="text-muted-foreground">$ </span>
      <span className="inline-block h-[1.1em] w-[0.55em] translate-y-[0.15em] bg-foreground/70 motion-safe:animate-pulse" />
    </p>
  );
}

function SetupSession({ compact }: { compact?: boolean }) {
  return (
    <>
      <Cmd>multica setup</Cmd>
      <p>Configured for Multica Cloud (https://multica.ai).</p>
      <p>{"  server_url: https://api.multica.ai"}</p>
      <p>{"  app_url:    https://multica.ai"}</p>
      <Gap />
      <p>Opening browser to authenticate...</p>
      <p>Authenticated as Leo Park (leo@acme.dev)</p>
      <p>Token saved to config.</p>
      <p>Daemon started (pid 48211, version v0.4.31)</p>
      <Gap />
      <p className="text-foreground">
        <span className="text-success">✓</span> Setup complete! Your machine is now connected to Multica.
      </p>
      <Gap />
      {compact ? null : (
        <>
          <Cmd>multica daemon status</Cmd>
          <p>{"Daemon:      running (pid 48211, uptime 14s)"}</p>
          <p>{"Version:     v0.4.31"}</p>
          <p>{"Agents:      claude, codex, opencode"}</p>
          <p>{"Workspaces:  1"}</p>
          <Gap />
        </>
      )}
      <Cursor />
    </>
  );
}

const MACHINES = [
  { icon: Server, name: "build-01", detail: "Linux · Leo Park", agents: ["Claude Code", "Codex", "OpenCode"], fresh: true },
  { icon: Laptop, name: "Mia’s MacBook Pro", detail: "macOS · Mia Chen", agents: ["Claude Code", "Cursor"] },
  { icon: Laptop, name: "Sam’s ThinkPad", detail: "Windows · Sam Rivera", agents: ["Codex"] },
];

function Marks({ names }: { names: string[] }) {
  return (
    <span className="flex shrink-0 -space-x-1">
      {names.map((name) => {
        const Mark = PROVIDER_MARKS.get(name);
        return (
          <span key={name} className="inline-flex size-5 items-center justify-center rounded-full bg-background ring-1 ring-border">
            {Mark ? <Mark className="size-3" /> : null}
          </span>
        );
      })}
    </span>
  );
}

function RuntimesPanel({ className, compact }: { className: string; compact?: boolean }) {
  return (
    <div className={cn(FLOATING, "absolute overflow-hidden", className)}>
      <div className="flex h-11 items-center gap-2 border-b border-border px-4">
        <Monitor className="size-4 text-muted-foreground" />
        <span className="text-label font-medium">Runtimes</span>
      </div>
      <GroupBand title="Machines" count={compact ? 2 : MACHINES.length} />
      {MACHINES.slice(0, compact ? 2 : undefined).map((machine) => (
        <div key={machine.name} className={cn("flex h-[52px] items-center gap-3 border-b border-border px-4 last:border-b-0", machine.fresh && "bg-brand/[0.04]")}>
          <span className="relative flex size-7 shrink-0 items-center justify-center rounded-lg bg-muted">
            <machine.icon className="size-3.5 text-muted-foreground" />
            <span className="absolute -right-0.5 -bottom-0.5 size-2 rounded-full bg-success ring-2 ring-background" />
          </span>
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-1.5 text-label font-medium">
              {machine.name}
              {machine.fresh ? <span className="rounded bg-brand/10 px-1 text-micro font-medium text-brand">New</span> : null}
            </span>
            <span className="block truncate text-caption text-muted-foreground">{machine.detail}</span>
          </span>
          {compact ? null : <Marks names={machine.agents} />}
        </div>
      ))}
    </div>
  );
}

const SCENE = { width: 1100, height: 600 } as const;
const SCENE_NARROW = { width: 440, height: 440 } as const;

/** Hero: `multica setup` connecting a Linux box, which then shows up on the Runtimes page. */
export function CliScene() {
  return (
    <>
      <div className="relative aspect-square sm:hidden">
        <ZoomCanvas {...SCENE_NARROW}>
          <TerminalWindow title="leo@build-01" className="absolute top-6 left-6 -right-10 -bottom-10">
            <SetupSession compact />
          </TerminalWindow>
          <RuntimesPanel compact className="right-4 bottom-5 w-[270px]" />
        </ZoomCanvas>
      </div>
      <div className="relative hidden aspect-[1100/600] sm:block">
        <ZoomCanvas {...SCENE}>
          <TerminalWindow title="leo@build-01" className="absolute top-10 left-[6%] w-[56%] -bottom-12">
            <SetupSession />
          </TerminalWindow>
          <RuntimesPanel className="top-[20%] right-[5%] w-[340px]" />
        </ZoomCanvas>
      </div>
    </>
  );
}

function ToolCall({ name, children }: { name: string; children: ReactNode }) {
  return (
    <p className="text-foreground">
      <span className="text-success">●</span> <span className="font-semibold">{name}</span>{" "}
      <span className="text-muted-foreground">{children}</span>
    </p>
  );
}

/** A coding agent working an issue through the CLI, the way the Multica CLI skill teaches it to. */
export function AgentSessionVisual() {
  return (
    <TerminalWindow title="Claude Code — acme/web" className="h-[340px] w-[420px]">
      <p className="text-foreground">
        <span className="text-muted-foreground">&gt; </span>Pick up ACM-163 and post what you find
      </p>
      <Gap />
      <ToolCall name="Bash">multica issue get ACM-163 --output json</ToolCall>
      <ToolCall name="Bash">multica issue comment list ACM-163 \</ToolCall>
      <p>{"      --roots-only --summary --compact"}</p>
      <ToolCall name="Read">e2e/checkout.spec.ts</ToolCall>
      <Gap />
      <p className="whitespace-normal! text-foreground">
        The test waits on a fixed 500 ms timeout before the payment form renders. I’ll wait for the form instead.
      </p>
      <Gap />
      <ToolCall name="Write">./reply.md</ToolCall>
      <ToolCall name="Bash">multica issue comment add ACM-163 \</ToolCall>
      <p>{"      --content-file ./reply.md"}</p>
      <p>{"  Comment added to issue ACM-163."}</p>
    </TerminalWindow>
  );
}
