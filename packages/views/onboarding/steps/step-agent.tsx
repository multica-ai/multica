"use client";

import { useRef, useState } from "react";
import { ArrowLeft, ArrowRight, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import { api } from "@multica/core/api";
import {
  recommendTemplate,
  type AgentTemplateId,
  type QuestionnaireAnswers,
} from "@multica/core/onboarding";
import type {
  Agent,
  AgentRuntime,
  CreateAgentRequest,
} from "@multica/core/types";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";

/**
 * Step 4 — create the user's first agent.
 *
 * Picks a recommended template from the questionnaire answers
 * (`recommendTemplate()` maps role × use_case → one of 4 templates),
 * attaches the template's default name + instructions, and ships a
 * ready-to-work agent on Create. Layout mirrors Questionnaire /
 * Workspace: a 2-column editorial shell with DragStrip + 3-region
 * app column (header / scrollable main / footer) + "About agents"
 * side panel hidden below lg.
 *
 * No rename, runtime-swap, or instructions editor on this step —
 * every template defaults are good enough to ship immediately, and
 * the agent settings page handles all customization post-onboarding.
 * Intentional: minimizing surface area keeps time-to-first-agent low.
 *
 * No skip path either — if the user arrived here they have a runtime
 * (Step 3 only routes to Step 4 when a runtime was picked), so
 * creating an agent is the purpose of this step. Users who want a
 * runtime-less workspace skip out at Step 3.
 */
interface AgentTemplate {
  id: AgentTemplateId;
  label: string;
  defaultName: string;
  emoji: string;
  blurb: string;
  instructions: string;
}

const AGENT_TEMPLATES: readonly AgentTemplate[] = [
  {
    id: "coding",
    label: "Coding Agent",
    defaultName: "Atlas",
    emoji: "⌘",
    blurb: "Writes, refactors, and ships code. Reads your repo.",
    instructions:
      "You are a Coding Agent on a product team. Pick up coding issues — implement features, fix bugs, write tests, and open pull requests. Read the repository before you start, follow existing code conventions, and keep diffs focused. Ask for clarification when the acceptance criteria are ambiguous.",
  },
  {
    id: "planning",
    label: "Planning Agent",
    defaultName: "Orion",
    emoji: "◐",
    blurb: "Breaks down work, drafts specs, keeps the board tidy.",
    instructions:
      "You are a Planning Agent. Turn loose ideas and open issues into scoped, ready-to-execute work: break them down into subtasks, write acceptance criteria, and propose owners and sequencing. Prefer clarity over speed. When blocked by missing context, ask one specific question rather than guessing.",
  },
  {
    id: "writing",
    label: "Writing Agent",
    defaultName: "Mira",
    emoji: "✎",
    blurb: "Drafts, summarizes, researches. Long-form friendly.",
    instructions:
      "You are a Writing Agent. Draft documents, summarize long content, and research topics on the web when needed. Structure your output as finished prose a reader can use directly — not an outline. Cite sources when you draw from them. Match the tone the user establishes in the issue.",
  },
  {
    id: "assistant",
    label: "Assistant",
    defaultName: "Vega",
    emoji: "✦",
    blurb: "General-purpose. Good default when the task is unclear.",
    instructions:
      "You are a general-purpose teammate. Handle varied tasks — light coding, writing, research, planning — and stay pragmatic about scope. When the task is ambiguous, ask one clarifying question before diving in. Default to short, useful outputs over exhaustive ones.",
  },
] as const;

const TEMPLATE_BY_ID: Record<AgentTemplateId, AgentTemplate> =
  Object.fromEntries(AGENT_TEMPLATES.map((t) => [t.id, t])) as Record<
    AgentTemplateId,
    AgentTemplate
  >;

export function StepAgent({
  runtime,
  questionnaire,
  onCreated,
  onBack,
}: {
  runtime: AgentRuntime;
  questionnaire: QuestionnaireAnswers;
  onCreated: (agent: Agent) => void | Promise<void>;
  onBack?: () => void;
}) {
  const recommendedId = recommendTemplate(questionnaire);
  const recommended = TEMPLATE_BY_ID[recommendedId];

  const [templateId, setTemplateId] =
    useState<AgentTemplateId>(recommendedId);
  const template = TEMPLATE_BY_ID[templateId];

  const [creating, setCreating] = useState(false);

  const handleCreate = async () => {
    if (creating) return;
    setCreating(true);
    try {
      const req: CreateAgentRequest = {
        name: template.defaultName,
        description: template.blurb,
        instructions: template.instructions,
        runtime_id: runtime.id,
        visibility: "workspace",
        template: templateId,
      };
      const agent = await api.createAgent(req);
      await onCreated(agent);
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to create agent",
      );
      setCreating(false);
    }
  };

  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);

  return (
    <div className="animate-onboarding-enter grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
      {/* Left column — DragStrip + 3-region app shell */}
      <div className="flex min-h-0 flex-col">
        <DragStrip />
        {/* Fixed header — Back + progress indicator */}
        <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
          {onBack ? (
            <button
              type="button"
              onClick={onBack}
              className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              Back
            </button>
          ) : (
            <span aria-hidden className="w-0" />
          )}
          <div className="flex-1">
            <StepHeader currentStep="agent" />
          </div>
        </header>

        {/* Scrollable middle. `useScrollFade` softly masks content at
            the header / footer edges as the user scrolls, replacing a
            hard divider line. */}
        <main
          ref={mainRef}
          style={fadeStyle}
          className="min-h-0 flex-1 overflow-y-auto"
        >
          <div className="mx-auto w-full max-w-[620px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14">
            <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
              Your first agent
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              Meet your first teammate.
            </h1>
            <p className="mt-4 text-[15.5px] leading-[1.55] text-foreground/80">
              Your answers point to a{" "}
              <strong className="font-medium text-foreground">
                {recommended.label}
              </strong>
              . Pick whichever of the four fits you — each template ships
              ready to take its first issue. You can retune its
              instructions from the agent settings page later.
            </p>

            <div className="mt-10 grid grid-cols-1 gap-3 sm:grid-cols-2">
              {AGENT_TEMPLATES.map((t) => (
                <TemplateCard
                  key={t.id}
                  template={t}
                  selected={templateId === t.id}
                  recommended={recommendedId === t.id}
                  onSelect={() => setTemplateId(t.id)}
                />
              ))}
            </div>
          </div>
        </main>

        {/* Fixed footer — hint + Create CTA. No skip path: reaching
            Step 4 means a runtime was picked at Step 3, so creating
            the agent IS this step. */}
        <footer className="flex shrink-0 items-center justify-between gap-4 bg-background px-6 py-4 sm:px-10 md:px-14 lg:px-16">
          <span className="hidden text-xs text-muted-foreground sm:block">
            One agent is enough to start. Add more from the sidebar later.
          </span>
          <Button size="lg" onClick={handleCreate} disabled={creating}>
            {creating && <Loader2 className="h-4 w-4 animate-spin" />}
            Create {template.defaultName}
            <ArrowRight className="h-4 w-4" />
          </Button>
        </footer>
      </div>

      {/* Right — About agents side panel, independent scroll */}
      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          <AboutAgentsSide />
        </div>
      </aside>
    </div>
  );
}

function TemplateCard({
  template,
  selected,
  recommended,
  onSelect,
}: {
  template: AgentTemplate;
  selected: boolean;
  recommended: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      role="radio"
      aria-checked={selected}
      onClick={onSelect}
      className={cn(
        "flex flex-col items-start gap-3 rounded-lg border bg-card px-4 py-4 text-left transition-all",
        selected
          ? "border-foreground shadow-[inset_0_0_0_1px_var(--color-foreground)]"
          : "hover:border-foreground/20 hover:bg-accent/30",
      )}
    >
      <div className="flex w-full items-start justify-between gap-2">
        <span
          aria-hidden
          className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-muted/70 font-serif text-lg text-foreground/80"
        >
          {template.emoji}
        </span>
        {recommended && (
          <span className="shrink-0 rounded-full bg-brand/10 px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider text-brand">
            Recommended
          </span>
        )}
      </div>
      <div className="flex flex-col gap-1">
        <div className="text-sm font-medium text-foreground">
          {template.label}
        </div>
        <p className="text-xs leading-snug text-muted-foreground">
          {template.blurb}
        </p>
      </div>
    </button>
  );
}

function AboutAgentsSide() {
  return (
    <div className="flex max-w-[380px] flex-col gap-8">
      <section className="flex flex-col gap-4">
        <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          What&apos;s an agent
        </div>
        <h2 className="font-serif text-[22px] font-medium leading-[1.25] tracking-tight text-foreground">
          An AI teammate that lives in your workspace.
        </h2>
        <p className="text-[14px] leading-[1.6] text-foreground/80">
          Agents show up in every assignee picker, just like any other
          colleague — except they can work 24/7 on whatever runtime you
          give them.
        </p>
      </section>

      <section className="flex flex-col gap-4">
        <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          Ways to work with an agent
        </div>
        <div className="flex flex-col gap-4">
          <WayItem
            glyph="→"
            title="Assign it an issue"
            body="It picks up the task and reports back in the thread."
          />
          <WayItem
            glyph="@"
            title="@mention in a comment"
            body="Pull it into a conversation for a quick take."
          />
          <WayItem
            glyph="◯"
            title="Chat one-on-one"
            body="Ask quick questions without creating an issue."
          />
          <WayItem
            glyph="↻"
            title="Put it on Autopilot"
            body="Daily triage, weekly digest, monthly audit — on a schedule."
          />
        </div>
      </section>

      <p className="text-[13px] leading-[1.55] text-muted-foreground">
        Add more agents anytime. A small team of specialized agents beats
        one jack-of-all-trades.
      </p>
    </div>
  );
}

function WayItem({
  glyph,
  title,
  body,
}: {
  glyph: string;
  title: string;
  body: string;
}) {
  return (
    <div className="grid grid-cols-[22px_1fr] gap-3">
      <div
        aria-hidden
        className="flex h-[20px] w-[20px] items-center justify-center text-[14px] text-muted-foreground"
      >
        {glyph}
      </div>
      <div className="flex flex-col gap-1">
        <div className="text-[14px] font-medium leading-tight text-foreground">
          {title}
        </div>
        <p className="text-[13px] leading-[1.5] text-muted-foreground">
          {body}
        </p>
      </div>
    </div>
  );
}
