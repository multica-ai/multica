"use client";

import { useMemo } from "react";
import {
  Brush,
  ChevronRight,
  FileText,
  FlaskConical,
  LayoutDashboard,
  ListChecks,
  Loader2,
  Megaphone,
  Palette,
  PenLine,
  Presentation,
  Search,
  Sparkles,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { agentTemplateListOptions } from "@multica/core/agents/queries";
import type { AgentTemplateSummary } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

interface TemplatePickerProps {
  /** Fired when a template card is clicked. The dialog advances to the
   *  detail step (which shows instructions + skills + Use button). */
  onSelect: (template: AgentTemplateSummary) => void;
}

/**
 * Step 2 of the create-agent flow: a 2-column grid of template cards,
 * grouped by `category`. Clicking a card moves to the detail step.
 *
 * Templates are a static catalog (workspace-independent, only changes on
 * server deploy), so the catalog is loaded through TanStack Query with
 * `staleTime: Infinity` — re-opening the picker hits the cache instantly
 * and there's no per-mount refetch.
 *
 * Icons and accent colors come from the template JSON itself (`icon` is a
 * lucide-react name, `accent` is a Multica semantic token). Resolved
 * through static maps (ICONS / ACCENTS) so Tailwind can JIT-scan every
 * class variant — dynamic `bg-${accent}/10` strings would silently not
 * generate.
 */
export function TemplatePicker({ onSelect }: TemplatePickerProps) {
  const { t } = useT("agents");
  const { data: templates = [], isLoading, error } = useQuery(
    agentTemplateListOptions(),
  );

  // Group by category. Templates without a category fall into the
  // localised "Other" bucket so they still render. Preserves the load
  // order within each group for deterministic UI (matches the
  // alphabetic-by-filename order the loader uses on the server).
  const otherCategory = t(($) => $.create_dialog.template_picker.other_category);
  const groups = useMemo(() => {
    const byCategory = new Map<string, AgentTemplateSummary[]>();
    for (const tmpl of templates) {
      const key = tmpl.category?.trim() ? tmpl.category : otherCategory;
      if (!byCategory.has(key)) byCategory.set(key, []);
      byCategory.get(key)!.push(tmpl);
    }
    return Array.from(byCategory.entries());
  }, [templates, otherCategory]);

  if (isLoading) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    );
  }
  if (error) {
    return (
      <div className="flex flex-1 items-center justify-center p-6">
        <div className="text-sm text-destructive">
          {error instanceof Error
            ? error.message
            : t(($) => $.create_dialog.template_picker.load_failed)}
        </div>
      </div>
    );
  }
  if (templates.length === 0) {
    return (
      <div className="flex flex-1 items-center justify-center p-6">
        <div className="text-sm text-muted-foreground">
          {t(($) => $.create_dialog.template_picker.empty)}
        </div>
      </div>
    );
  }

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="mx-auto max-w-5xl space-y-6 p-6">
        {groups.map(([category, tmpls]) => (
          <section key={category}>
            <h2 className="sticky top-0 z-10 -mx-6 border-b bg-background px-6 py-2 text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              {category}
            </h2>
            <div className="grid grid-cols-1 gap-3 pt-3 md:grid-cols-2">
              {tmpls.map((tmpl) => (
                <TemplateCard
                  key={tmpl.slug}
                  template={tmpl}
                  onClick={() => onSelect(tmpl)}
                />
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}

interface TemplateCardProps {
  template: AgentTemplateSummary;
  onClick: () => void;
}

function TemplateCard({ template, onClick }: TemplateCardProps) {
  const { t } = useT("agents");
  const Icon = ICONS[template.icon ?? ""] ?? FileText;
  const accentClass = ACCENTS[template.accent ?? ""] ?? ACCENTS.muted;

  return (
    <button
      type="button"
      onClick={onClick}
      className="group flex items-start gap-3 rounded-lg border bg-card p-4 text-left transition-colors hover:border-primary/40 hover:bg-accent/40"
    >
      <div
        className={cn(
          "flex h-10 w-10 shrink-0 items-center justify-center rounded-lg",
          accentClass,
        )}
      >
        <Icon className="h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-1">
          <span className="truncate text-sm font-semibold">{template.name}</span>
          <ChevronRight className="ml-auto h-4 w-4 shrink-0 text-muted-foreground/40 transition-transform group-hover:translate-x-0.5 group-hover:text-muted-foreground" />
        </div>
        <p className="mt-1 line-clamp-2 text-xs text-muted-foreground">
          {template.description}
        </p>
        <div className="mt-2.5 inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-[10px] font-medium text-muted-foreground">
          {t(($) => $.create_dialog.template_card.skills, {
            count: template.skills.length,
          })}
        </div>
      </div>
    </button>
  );
}

// --- Static maps so Tailwind's JIT scanner picks up every variant ---

/** Lucide icon name → component. Add new entries when shipping templates
 *  that use icons not yet listed here. Unknown names fall back to FileText. */
const ICONS: Record<string, LucideIcon> = {
  Search,
  Palette,
  FileText,
  FlaskConical,
  Sparkles,
  ListChecks,
  Brush,
  PenLine,
  Megaphone,
  Presentation,
  LayoutDashboard,
};

/** Semantic accent → Tailwind class string. The class strings are written
 *  out verbatim so JIT scans them; dynamic `bg-${name}/10` would not be
 *  generated. Mirrors the conventions in runtime-columns.tsx /
 *  usage-section.tsx (existing uses of these tokens). */
const DEFAULT_ACCENT = "bg-muted text-muted-foreground";

const ACCENTS: Record<string, string> = {
  info: "bg-info/10 text-info",
  success: "bg-success/10 text-success",
  warning: "bg-warning/10 text-warning",
  primary: "bg-primary/10 text-primary",
  secondary: "bg-secondary text-secondary-foreground",
  muted: DEFAULT_ACCENT,
};

/** Exposed for the detail / form steps so they can render the same icon
 *  badge as the picker card. Keeps visual continuity across steps. */
export function getTemplateIcon(iconName: string | undefined): LucideIcon {
  return ICONS[iconName ?? ""] ?? FileText;
}

export function getAccentClass(accent: string | undefined): string {
  return ACCENTS[accent ?? ""] ?? DEFAULT_ACCENT;
}
