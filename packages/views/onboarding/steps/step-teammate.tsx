"use client";

import { useRef, useState, type ComponentType } from "react";
import {
  ArrowLeft,
  ArrowRight,
  CheckCircle2,
  Loader2,
  MessageSquareText,
  Settings2,
  Sparkles,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { cn } from "@multica/ui/lib/utils";
import type { AgentRuntime } from "@multica/core/types";
import { DragStrip } from "@multica/views/platform";
import { StepHeader } from "../components/step-header";
import { RuntimeAsidePanel } from "../components/runtime-aside-panel";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { useT } from "../../i18n";

const MULTICA_HELPER_AVATAR_URL =
  "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 128 128'%3E%3Crect width='128' height='128' rx='30' fill='%23111217'/%3E%3Cpath d='M28 76c8-22 22-33 42-33 15 0 26 7 32 20' fill='none' stroke='%23ffffff' stroke-width='10' stroke-linecap='round'/%3E%3Cpath d='M38 88c13 13 39 17 58 1' fill='none' stroke='%238EE3C8' stroke-width='8' stroke-linecap='round'/%3E%3Ccircle cx='48' cy='56' r='7' fill='%23ffffff'/%3E%3Ccircle cx='78' cy='56' r='7' fill='%23ffffff'/%3E%3Cpath d='M64 20v14' stroke='%238EE3C8' stroke-width='8' stroke-linecap='round'/%3E%3Ccircle cx='64' cy='16' r='6' fill='%238EE3C8'/%3E%3C/svg%3E";

export function StepTeammate({
  runtime,
  onCreate,
  onBack,
}: {
  runtime: AgentRuntime;
  onCreate: () => void | Promise<void>;
  onBack?: () => void;
}) {
  const { t } = useT("onboarding");
  const mainRef = useRef<HTMLElement>(null);
  const fadeStyle = useScrollFade(mainRef);
  const [creating, setCreating] = useState(false);

  const handleCreate = async () => {
    if (creating) return;
    setCreating(true);
    try {
      await onCreate();
    } catch {
      // The parent owns the toast. This step only restores the button state.
    } finally {
      setCreating(false);
    }
  };

  return (
    <div className="animate-onboarding-enter grid h-full min-h-0 grid-cols-1 lg:grid-cols-[minmax(0,1fr)_480px]">
      <div className="flex min-h-0 flex-col">
        <DragStrip />

        <header className="flex shrink-0 items-center gap-4 bg-background px-6 py-3 sm:px-10 md:px-14 lg:px-16">
          {onBack ? (
            <button
              type="button"
              onClick={onBack}
              disabled={creating}
              className="flex items-center gap-1.5 text-sm text-muted-foreground transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              <ArrowLeft className="h-3.5 w-3.5" />
              {t(($) => $.common.back)}
            </button>
          ) : (
            <span aria-hidden className="w-0" />
          )}
          <div className="flex-1">
            <StepHeader currentStep="teammate" />
          </div>
        </header>

        <main
          ref={mainRef}
          style={fadeStyle}
          className="min-h-0 flex-1 overflow-y-auto"
        >
          <div className="mx-auto w-full max-w-[660px] px-6 py-10 sm:px-10 md:px-14 lg:px-0 lg:py-14">
            <div className="mb-2 text-xs font-medium uppercase tracking-[0.08em] text-muted-foreground">
              {t(($) => $.step_teammate.eyebrow)}
            </div>
            <h1 className="text-balance font-serif text-[36px] font-medium leading-[1.1] tracking-tight text-foreground">
              {t(($) => $.step_teammate.headline)}
            </h1>
            <p className="mt-4 max-w-[580px] text-[15.5px] leading-[1.55] text-muted-foreground">
              {t(($) => $.step_teammate.lede, { runtime: runtime.name })}
            </p>

            <section className="mt-10 overflow-hidden rounded-xl border bg-card">
              <div className="flex flex-col gap-6 p-6 sm:flex-row sm:items-center">
                <img
                  src={MULTICA_HELPER_AVATAR_URL}
                  alt={t(($) => $.step_teammate.avatar_label)}
                  className="h-24 w-24 shrink-0 rounded-2xl shadow-sm"
                />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <h2 className="text-2xl font-semibold tracking-tight text-foreground">
                      {t(($) => $.step_teammate.name)}
                    </h2>
                    <span className="inline-flex items-center rounded-full border bg-muted px-2.5 py-1 text-[12px] font-medium text-muted-foreground">
                      {t(($) => $.step_teammate.role)}
                    </span>
                  </div>
                  <div className="mt-3 inline-flex max-w-full items-center gap-2 rounded-md bg-muted/70 px-3 py-2 text-sm text-muted-foreground">
                    <ProviderLogo provider={runtime.provider} className="h-4 w-4 shrink-0" />
                    <span className="truncate">{runtime.name}</span>
                    <span
                      className={cn(
                        "h-1.5 w-1.5 shrink-0 rounded-full",
                        runtime.status === "online"
                          ? "bg-success"
                          : "bg-muted-foreground/40",
                      )}
                      aria-hidden
                    />
                  </div>
                </div>
              </div>

              <div className="grid border-t bg-muted/20 sm:grid-cols-3">
                <TeammatePoint
                  icon={MessageSquareText}
                  label={t(($) => $.step_teammate.point_issue)}
                />
                <TeammatePoint
                  icon={Sparkles}
                  label={t(($) => $.step_teammate.point_context)}
                />
                <TeammatePoint
                  icon={Settings2}
                  label={t(($) => $.step_teammate.point_customize)}
                />
              </div>
            </section>

            <div className="mt-8 flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
              <span
                aria-live="polite"
                className="mr-auto text-xs text-muted-foreground"
              >
                {t(($) => $.step_teammate.footer_hint)}
              </span>
              <Button size="lg" disabled={creating} onClick={handleCreate}>
                {creating ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <CheckCircle2 className="h-4 w-4" />
                )}
                {t(($) => $.step_teammate.create_action)}
                <ArrowRight className="h-4 w-4" />
              </Button>
            </div>
          </div>
        </main>
      </div>

      <aside className="hidden min-h-0 border-l bg-muted/40 lg:flex lg:flex-col">
        <DragStrip />
        <div className="min-h-0 flex-1 overflow-y-auto px-12 py-12">
          <RuntimeAsidePanel />
        </div>
      </aside>
    </div>
  );
}

function TeammatePoint({
  icon: Icon,
  label,
}: {
  icon: ComponentType<{ className?: string }>;
  label: string;
}) {
  return (
    <div className="flex min-h-[104px] items-start gap-3 border-t p-5 first:border-t-0 sm:border-l sm:border-t-0 sm:first:border-l-0">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md bg-background text-foreground">
        <Icon className="h-4 w-4" />
      </div>
      <p className="text-sm leading-[1.45] text-foreground">{label}</p>
    </div>
  );
}
