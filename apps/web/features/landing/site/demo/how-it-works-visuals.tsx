"use client";

import type { ReactNode } from "react";
import { Check, ChevronDown, ChevronRight, GitMerge, GitPullRequest, Laptop, Search } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { StatusIcon } from "@multica/views/issues/components/status-icon";
import {
  Avatar,
  BACKEND,
  ENTER,
  LEO,
  MIA,
  PROVIDER_MARKS,
  QA,
  Window,
  Working,
  ZoomCanvas,
  useStoryLoop,
} from "./mock-kit";

/*
 * Mockups for the home page "how it works" steps, on the same canvas as the
 * large feature cards: 520 × 390, growing on wide cards (see ZoomCanvas).
 * Each loops its step while on screen and holds the finished frame under
 * reduced motion. Data is fictional.
 */

const CANVAS = { width: 520, height: 390 } as const;

// --- 01 Connect -------------------------------------------------------------
// `multica setup` signs in, finds the coding agents, and brings the machine
// online.

const AGENTS_FOUND = ["Claude Code", "Codex", "Cursor"];

const SETUP_LINES: ReactNode[] = [
  <span key="command">
    <span className="text-muted-foreground">~ $</span> multica setup
  </span>,
  <span key="auth">
    <span className="text-success">✓</span> Signed in as mia@acme.dev
  </span>,
  <span key="workspace">
    <span className="text-success">✓</span> Workspace <span className="text-muted-foreground">Acme Labs</span>
  </span>,
  <span key="scan" className="text-muted-foreground">
    Looking for coding agents…
  </span>,
  ...AGENTS_FOUND.map((name) => {
    const Mark = PROVIDER_MARKS.get(name);
    return (
      <span key={name} className="pl-4">
        <span className="text-success">✓</span> {Mark ? <Mark className="inline size-3 align-[-2px]" /> : null} {name}
      </span>
    );
  }),
  <span key="online">
    <span className="text-success">✓</span> Mia’s MacBook Pro is online
  </span>,
];

const CONNECT_HOLDS = [...SETUP_LINES.map((_, index) => (index === 0 ? 900 : 380)), 2800, 500] as const;

export function ConnectVisual() {
  const { ref, step } = useStoryLoop(CONNECT_HOLDS, SETUP_LINES.length);
  const shown = Math.min(step + 1, SETUP_LINES.length);
  const online = step >= SETUP_LINES.length;
  const reset = step === SETUP_LINES.length + 1;
  return (
    <ZoomCanvas {...CANVAS}>
      <div ref={ref} className={cn("absolute inset-0 transition-opacity duration-300", reset && "opacity-0")}>
        <Window className="inset-x-7 top-7 -bottom-8">
          <div className="flex h-9 shrink-0 items-center gap-1.5 border-b border-border px-3">
            {[0, 1, 2].map((dot) => (
              <span key={dot} className="size-2.5 rounded-full bg-muted-foreground/25" />
            ))}
            <span className="flex-1 pr-10 text-center text-caption text-muted-foreground">mia — zsh</span>
          </div>
          <div className="flex flex-col gap-1.5 p-4 font-mono text-caption">
            {SETUP_LINES.slice(0, shown).map((line, index) => (
              <p key={index} className="animate-in fade-in duration-200 motion-reduce:animate-none">
                {line}
              </p>
            ))}
          </div>
        </Window>
        {online ? (
          <div
            className={cn(
              ENTER,
              "absolute right-12 bottom-8 flex w-[280px] items-center gap-3 rounded-xl border border-site-line-strong bg-background p-3 shadow-site-float",
            )}
          >
            <span className="flex size-8 items-center justify-center rounded-lg bg-muted">
              <Laptop className="size-4 text-muted-foreground" />
            </span>
            <div className="min-w-0 flex-1">
              <p className="flex items-center gap-2 text-label font-medium">
                Mia’s MacBook Pro
                <span className="size-1.5 rounded-full bg-success" />
              </p>
              <p className="text-caption text-muted-foreground">Online · 3 coding agents</p>
            </div>
          </div>
        ) : null}
      </div>
    </ZoomCanvas>
  );
}

// --- 02 Add agents ----------------------------------------------------------
// Fill in a new agent, create it, and find it in the assignee picker beside
// the people on the team.

const AGENT_HOLDS = [700, 700, 700, 900, 700, 2800, 500] as const;

function Field({ label, filled, children }: { label: string; filled: boolean; children: ReactNode }) {
  return (
    <div>
      <p className="mb-1 text-caption text-muted-foreground">{label}</p>
      <div className="flex min-h-8 items-center gap-2 rounded-md border border-border bg-background px-2.5 py-1.5 text-caption">
        {filled ? <span className="flex min-w-0 flex-1 items-center gap-2 animate-in fade-in duration-300 motion-reduce:animate-none">{children}</span> : null}
      </div>
    </div>
  );
}

function PickerRow({ children, active }: { children: ReactNode; active?: boolean }) {
  return (
    <div className={cn("flex h-8 items-center gap-2 rounded-md px-2 text-caption", active && "bg-muted")}>{children}</div>
  );
}

export function AgentVisual() {
  const { ref, step } = useStoryLoop(AGENT_HOLDS, 5);
  const ClaudeMark = PROVIDER_MARKS.get("Claude Code");
  const created = step >= 5;
  return (
    <ZoomCanvas {...CANVAS}>
      <div ref={ref} className={cn("absolute inset-0 transition-opacity duration-300", step === 6 && "opacity-0")}>
        <Window className="top-7 left-7 -bottom-8 w-[340px]">
          <div className="flex h-11 shrink-0 items-center border-b border-border px-4 text-label font-medium">
            Create agent
          </div>
          <div className="flex flex-col gap-3 p-4">
            <Field label="Name" filled={step >= 1}>
              <span className="font-medium">Backend Agent</span>
            </Field>
            <Field label="Runtime" filled={step >= 2}>
              {ClaudeMark ? <ClaudeMark className="size-3.5" /> : null}
              <span className="min-w-0 flex-1 truncate">Claude Code on Mia’s MacBook Pro</span>
              <ChevronDown className="size-3.5 text-muted-foreground" />
            </Field>
            <Field label="Instructions" filled={step >= 3}>
              <span className="text-muted-foreground">Own the API and its tests. Open a PR for every change.</span>
            </Field>
            <Field label="Skills" filled={step >= 3}>
              {["release-checklist", "api-style-guide"].map((skill) => (
                <span key={skill} className="rounded bg-muted px-1.5 py-0.5 font-mono text-micro">
                  {skill}
                </span>
              ))}
            </Field>
            <span
              className={cn(
                "mt-1 flex h-8 items-center justify-center rounded-md bg-primary text-caption font-medium text-primary-foreground transition-transform duration-150",
                step === 4 && "scale-[0.97]",
              )}
            >
              {created ? (
                <>
                  <Check className="mr-1 size-3.5" /> Created
                </>
              ) : (
                "Create agent"
              )}
            </span>
          </div>
        </Window>
        {created ? (
          <div
            className={cn(
              ENTER,
              "absolute top-24 right-6 w-[220px] rounded-xl border border-site-line-strong bg-background p-1.5 shadow-site-float",
            )}
          >
            <div className="flex h-8 items-center gap-2 border-b border-border px-2 pb-1.5 text-caption text-muted-foreground">
              <Search className="size-3.5" /> Assign to…
            </div>
            <p className="px-2 pt-2 pb-1 text-micro text-muted-foreground">Members</p>
            <PickerRow>
              <Avatar actor={MIA} size={20} /> Mia Chen
            </PickerRow>
            <PickerRow>
              <Avatar actor={LEO} size={20} /> Leo Park
            </PickerRow>
            <p className="px-2 pt-2 pb-1 text-micro text-muted-foreground">Agents</p>
            <PickerRow active>
              <Avatar actor={BACKEND} size={20} />
              <span className="flex-1">Backend Agent</span>
              <span className="rounded bg-brand/10 px-1.5 text-micro text-brand">New</span>
            </PickerRow>
            <PickerRow>
              <Avatar actor={QA} size={20} /> QA Agent
            </PickerRow>
          </div>
        ) : null}
      </div>
    </ZoomCanvas>
  );
}

// --- 03 Assign and review ---------------------------------------------------
// The agent takes the issue, runs, opens a PR, and a person approves and
// merges it.

const REVIEW_HOLDS = [1100, 1500, 2000, 1300, 2600, 500] as const;

function ActivityLine({ icon, children }: { icon: ReactNode; children: ReactNode }) {
  return (
    <div className={cn(ENTER, "flex items-center gap-2 text-caption text-muted-foreground")}>
      <span className="flex size-4 items-center justify-center">{icon}</span>
      <span className="min-w-0 flex-1">{children}</span>
    </div>
  );
}

export function ReviewVisual() {
  const { ref, step } = useStoryLoop(REVIEW_HOLDS, 4);
  const ClaudeMark = PROVIDER_MARKS.get("Claude Code");
  const merged = step >= 4;
  return (
    <ZoomCanvas {...CANVAS}>
      <div ref={ref} className={cn("absolute inset-0 transition-opacity duration-300", step === 5 && "opacity-0")}>
        <Window className="inset-x-7 top-7 -bottom-8">
          <div className="flex h-11 shrink-0 items-center gap-1 border-b border-border px-4 text-caption text-muted-foreground">
            Acme Labs <ChevronRight className="size-3" /> <span className="text-foreground">ACM-158</span>
          </div>
          <div className="flex flex-col gap-3 px-5 py-4">
            <div className="flex items-center gap-2">
              <StatusIcon status={merged ? "done" : step >= 3 ? "in_review" : "in_progress"} className="h-4 w-4" />
              <p className="text-title font-semibold leading-snug">Add CSV export to invoices</p>
            </div>
            <ActivityLine icon={<Avatar actor={MIA} size={16} />}>
              <span className="font-medium text-foreground">Mia Chen</span> assigned{" "}
              <span className="font-medium text-foreground">Backend Agent</span>
            </ActivityLine>
            {step >= 1 ? (
              <div className={cn(ENTER, "flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2 text-caption")}>
                {ClaudeMark ? <ClaudeMark className="size-3.5" /> : null}
                <span className="flex-1 font-medium">Run · Claude Code</span>
                {step === 1 ? (
                  <Working />
                ) : (
                  <span className="flex items-center gap-1 text-micro text-muted-foreground">
                    <Check className="size-3" /> Finished in 2m 51s
                  </span>
                )}
              </div>
            ) : null}
            {step >= 2 ? (
              <div className={cn(ENTER, "rounded-lg border border-surface-border bg-surface p-3 text-caption shadow-[var(--surface-shadow)]")}>
                <div className="mb-1 flex items-center gap-1.5">
                  <Avatar actor={BACKEND} size={16} />
                  <span className="font-medium">Backend Agent</span>
                </div>
                <p>CSV export sits in the existing Download menu. Tests cover empty and 10k-row invoices.</p>
                <div className="mt-2 flex items-center gap-2 rounded-md border border-border px-2.5 py-1.5">
                  {merged ? (
                    <GitMerge className="size-3.5 text-muted-foreground" />
                  ) : (
                    <GitPullRequest className="size-3.5 text-muted-foreground" />
                  )}
                  <span className="min-w-0 flex-1 truncate font-medium">#322 Add CSV export to invoices</span>
                  <span className="text-success tabular-nums">+96</span>
                  <span className="text-destructive tabular-nums">−8</span>
                </div>
                <div className="mt-2 flex items-center gap-2">
                  <span className="flex items-center gap-1 text-micro text-muted-foreground">
                    <Check className="size-3 text-success" /> Checks passed
                  </span>
                  <span className="ml-auto flex items-center gap-1.5">
                    <span
                      className={cn(
                        "flex h-6 items-center rounded-md border border-border px-2 text-micro transition-colors duration-300",
                        step >= 3 && "border-success/40 text-success",
                      )}
                    >
                      {step >= 3 ? "Approved" : "Approve"}
                    </span>
                    <span
                      className={cn(
                        "flex h-6 items-center rounded-md px-2 text-micro font-medium transition-colors duration-300",
                        merged ? "bg-muted text-muted-foreground" : "bg-primary text-primary-foreground",
                      )}
                    >
                      {merged ? "Merged" : "Merge"}
                    </span>
                  </span>
                </div>
              </div>
            ) : null}
            {merged ? (
              <ActivityLine icon={<GitMerge className="size-3.5" />}>
                <span className="font-medium text-foreground">Mia Chen</span> merged #322 and closed the issue
              </ActivityLine>
            ) : null}
          </div>
        </Window>
      </div>
    </ZoomCanvas>
  );
}
