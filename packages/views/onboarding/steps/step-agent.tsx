"use client";

import { useRef, useState } from "react";
import { ArrowLeft, ArrowRight, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { useT } from "@multica/i18n/react";
import { openExternal, publicAppUrl } from "../../platform";
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
  labelKey: string;
  nameKey: string;
  emoji: string;
  blurbKey: string;
  instructionsKey: string;
}

const AGENT_TEMPLATES: readonly AgentTemplate[] = [
  {
    id: "coding",
    labelKey: "sa_tmpl_coding_label",
    nameKey: "sa_tmpl_coding_name",
    emoji: "⌘",
    blurbKey: "sa_tmpl_coding_blurb",
    instructionsKey: "sa_tmpl_coding_instructions",
  },
  {
    id: "planning",
    labelKey: "sa_tmpl_planning_label",
    nameKey: "sa_tmpl_planning_name",
    emoji: "◐",
    blurbKey: "sa_tmpl_planning_blurb",
    instructionsKey: "sa_tmpl_planning_instructions",
  },
  {
    id: "writing",
    labelKey: "sa_tmpl_writing_label",
    nameKey: "sa_tmpl_writing_name",
    emoji: "✎",
    blurbKey: "sa_tmpl_writing_blurb",
    instructionsKey: "sa_tmpl_writing_instructions",
  },
  {
    id: "assistant",
    labelKey: "sa_tmpl_assistant_label",
    nameKey: "sa_tmpl_assistant_name",
    emoji: "✦",
    blurbKey: "sa_tmpl_assistant_blurb",
    instructionsKey: "sa_tmpl_assistant_instructions",
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
  const t = useT("onboarding");
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
        name: t(template.nameKey),
        description: t(template.blurbKey),
        instructions: t(template.instructionsKey),
        runtime_id: runtime.id,
        visibility: "workspace",
        template: templateId,
      };
      const agent = await api.createAgent(req);
      await onCreated(agent);
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : t("sa_toast_failed_create"),
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
              {t("sa_back")}
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
              {t("sa_eyebrow")}
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              {t("sa_title")}
            </h1>
            <p className="mt-4 text-[15.5px] leading-[1.55] text-foreground/80">
              {t("sa_recommended_intro")}{" "}
              <strong className="font-medium text-foreground">
                {t(recommended.labelKey)}
              </strong>
              {t("sa_recommended_hint")}
            </p>

            <div className="mt-10 grid grid-cols-1 gap-3 sm:grid-cols-2">
              {AGENT_TEMPLATES.map((tmpl) => (
                <TemplateCard
                  key={tmpl.id}
                  template={tmpl}
                  selected={templateId === tmpl.id}
                  recommended={recommendedId === tmpl.id}
                  onSelect={() => setTemplateId(tmpl.id)}
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
            {t("sa_footer_hint")}
          </span>
          <Button size="lg" onClick={handleCreate} disabled={creating}>
            {creating && <Loader2 className="h-4 w-4 animate-spin" />}
            {t("sa_create_button", { name: t(template.nameKey) })}
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
  const t = useT("onboarding");
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
            {t("sa_recommended_badge")}
          </span>
        )}
      </div>
      <div className="flex flex-col gap-1">
        <div className="text-sm font-medium text-foreground">
          {t(template.labelKey)}
        </div>
        <p className="text-xs leading-snug text-muted-foreground">
          {t(template.blurbKey)}
        </p>
      </div>
    </button>
  );
}

function AboutAgentsSide() {
  const t = useT("onboarding");
  return (
    <div className="flex max-w-[380px] flex-col gap-8">
      <section className="flex flex-col gap-4">
        <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t("sa_side_what_eyebrow")}
        </div>
        <h2 className="font-serif text-[22px] font-medium leading-[1.25] tracking-tight text-foreground">
          {t("sa_side_what_title")}
        </h2>
        <p className="text-[14px] leading-[1.6] text-foreground/80">
          {t("sa_side_what_body")}
        </p>
      </section>

      <section className="flex flex-col gap-4">
        <div className="text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
          {t("sa_side_ways_eyebrow")}
        </div>
        <div className="flex flex-col gap-4">
          <WayItem
            glyph="→"
            title={t("ag_assign")}
            body={t("sa_side_assign_body")}
          />
          <WayItem
            glyph="@"
            title={t("sa_side_mention_title")}
            body={t("sa_side_mention_body")}
          />
          <WayItem
            glyph="◯"
            title={t("ag_chat")}
            body={t("sa_side_chat_body")}
          />
          <WayItem
            glyph="↻"
            title={t("ag_autopilot")}
            body={t("sa_side_autopilot_body")}
          />
        </div>
      </section>

      <p className="text-[13px] leading-[1.55] text-muted-foreground">
        {t("sa_side_footer")}
      </p>

      <button
        type="button"
        onClick={() => openExternal(publicAppUrl("/docs/agents-create"))}
        className="self-start text-[13px] text-muted-foreground underline underline-offset-4 transition-colors hover:text-foreground"
      >
        {t("sa_side_docs_link")}
      </button>
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
