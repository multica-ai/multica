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
import { useT } from "../../i18n";

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

// Defaults stay constant (names + emoji are visual identity, not copy);
// label / blurb / instructions resolve from the bundle at render time.
const TEMPLATE_DEFAULTS: readonly Omit<AgentTemplate, "label" | "blurb" | "instructions">[] = [
  { id: "coding", defaultName: "Atlas", emoji: "⌘" },
  { id: "planning", defaultName: "Orion", emoji: "◐" },
  { id: "writing", defaultName: "Mira", emoji: "✎" },
  { id: "assistant", defaultName: "Vega", emoji: "✦" },
] as const;

function useAgentTemplates(): {
  templates: readonly AgentTemplate[];
  byId: Record<AgentTemplateId, AgentTemplate>;
} {
  const { t } = useT("onboarding");
  const templates = TEMPLATE_DEFAULTS.map((d) => ({
    ...d,
    label: t(($) => $.step_agent.templates[d.id].label),
    blurb: t(($) => $.step_agent.templates[d.id].blurb),
    instructions: t(($) => $.step_agent.templates[d.id].instructions),
  })) as readonly AgentTemplate[];
  const byId = Object.fromEntries(templates.map((tpl) => [tpl.id, tpl])) as Record<
    AgentTemplateId,
    AgentTemplate
  >;
  return { templates, byId };
}

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
  const { t } = useT("onboarding");
  const { templates: AGENT_TEMPLATES, byId: TEMPLATE_BY_ID } = useAgentTemplates();
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
        err instanceof Error ? err.message : t(($) => $.step_agent.create_failed),
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
              {t(($) => $.common.back)}
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
              {t(($) => $.step_agent.eyebrow)}
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              {t(($) => $.step_agent.headline)}
            </h1>
            <p className="mt-4 text-[15.5px] leading-[1.55] text-foreground/80">
              {t(($) => $.step_agent.lede_prefix)}
              <strong className="font-medium text-foreground">
                {recommended.label}
              </strong>
              {t(($) => $.step_agent.lede_suffix)}
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
            {t(($) => $.step_agent.footer_hint)}
          </span>
          <Button size="lg" onClick={handleCreate} disabled={creating}>
            {creating && <Loader2 className="h-4 w-4 animate-spin" />}
            {t(($) => $.step_agent.create_action, { name: template.defaultName })}
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
  const { t } = useT("onboarding");
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
            {t(($) => $.step_agent.recommended_badge)}
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
  const { t } = useT("onboarding");
  return (
    <div className="flex max-w-[380px] flex-col gap-8">
      <section className="flex flex-col gap-4">
        <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t(($) => $.step_agent.about_eyebrow)}
        </div>
        <h2 className="font-serif text-[22px] font-medium leading-[1.25] tracking-tight text-foreground">
          {t(($) => $.step_agent.about_headline)}
        </h2>
        <p className="text-[14px] leading-[1.6] text-foreground/80">
          {t(($) => $.step_agent.about_body)}
        </p>
      </section>

      <section className="flex flex-col gap-4">
        <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t(($) => $.step_agent.ways_eyebrow)}
        </div>
        <div className="flex flex-col gap-4">
          <WayItem
            glyph="→"
            title={t(($) => $.step_agent.way_assign_title)}
            body={t(($) => $.step_agent.way_assign_body)}
          />
          <WayItem
            glyph="@"
            title={t(($) => $.step_agent.way_mention_title)}
            body={t(($) => $.step_agent.way_mention_body)}
          />
          <WayItem
            glyph="◯"
            title={t(($) => $.step_agent.way_chat_title)}
            body={t(($) => $.step_agent.way_chat_body)}
          />
          <WayItem
            glyph="↻"
            title={t(($) => $.step_agent.way_autopilot_title)}
            body={t(($) => $.step_agent.way_autopilot_body)}
          />
        </div>
      </section>

      <p className="text-[13px] leading-[1.55] text-muted-foreground">
        {t(($) => $.step_agent.add_more_hint)}
      </p>

      <a
        href="https://multica.ai/docs/agents-create"
        target="_blank"
        rel="noopener noreferrer"
        className="self-start text-[13px] text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
      >
        {t(($) => $.step_agent.docs_link)}
      </a>
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
