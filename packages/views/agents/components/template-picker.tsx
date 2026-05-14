"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  AlignLeft,
  BookOpen,
  Brush,
  Briefcase,
  Bug,
  ChevronRight,
  ClipboardList,
  FileText,
  FlaskConical,
  GitCommit,
  GitPullRequest,
  GraduationCap,
  Highlighter,
  Languages,
  LayoutDashboard,
  Lightbulb,
  ListChecks,
  Loader2,
  Megaphone,
  MessageSquare,
  Microscope,
  Palette,
  PenLine,
  Presentation,
  Scale,
  Search,
  Sparkles,
  Target,
  Type,
  UserRound,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { agentTemplateListOptions } from "@multica/core/agents/queries";
import type { AgentTemplateSummary } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
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

  // `null` = "All" (default). When a specific category is selected, the
  // grid renders flat (no section headers) — the active pill already
  // tells the user what they're looking at, so headers would be noise.
  const [selectedCategory, setSelectedCategory] = useState<string | null>(null);

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

  // Templates currently visible given the filter. When "All" is active
  // we show every template (grouped by category below); otherwise we
  // only show the matching category.
  const visibleTemplates = useMemo(() => {
    if (selectedCategory === null) return templates;
    return templates.filter(
      (tmpl) =>
        (tmpl.category?.trim() ? tmpl.category : otherCategory) ===
        selectedCategory,
    );
  }, [templates, selectedCategory, otherCategory]);

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
      <div className="mx-auto max-w-5xl space-y-4 p-6">
        {/* Category filter — mirrors the IssuesHeader scope pattern
            (Button variant="outline" + active-class swap). `flex-wrap`
            so the 8 pills (All + 7 categories) degrade gracefully on
            narrow widths. Counts are inlined into the label rather than
            shown as a separate badge because we want the pill row to
            stay one-line-tall per pill. */}
        <div className="flex flex-wrap items-center gap-1">
          <FilterPill
            label={`${t(($) => $.create_dialog.template_picker.filter_all)} (${templates.length})`}
            active={selectedCategory === null}
            onClick={() => setSelectedCategory(null)}
          />
          {groups.map(([category, tmpls]) => (
            <FilterPill
              key={category}
              label={`${category} (${tmpls.length})`}
              active={selectedCategory === category}
              onClick={() => setSelectedCategory(category)}
            />
          ))}
        </div>

        {/* Grid — grouped with sticky headers when "All" is active;
            flat when a single category is filtered (the active pill
            already tells the user what they're looking at). */}
        {selectedCategory === null ? (
          <div className="space-y-6">
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
        ) : (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {visibleTemplates.map((tmpl) => (
              <TemplateCard
                key={tmpl.slug}
                template={tmpl}
                onClick={() => onSelect(tmpl)}
              />
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

/** Single filter pill. Visual matches IssuesHeader's scope toggle
 *  (Button outline + bg-accent on active) so the catalog feels
 *  consistent with the rest of the app's filter affordances. */
function FilterPill({
  label,
  active,
  onClick,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <Button
      type="button"
      variant="outline"
      size="sm"
      className={cn(
        "h-7 text-xs",
        active
          ? "bg-accent text-accent-foreground hover:bg-accent/80"
          : "text-muted-foreground",
      )}
      onClick={onClick}
    >
      {label}
    </Button>
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
          {template.skills.length === 0
            ? t(($) => $.create_dialog.template_card.prompt_only)
            : t(($) => $.create_dialog.template_card.skills, {
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
  AlertTriangle,
  AlignLeft,
  BookOpen,
  Briefcase,
  Brush,
  Bug,
  ClipboardList,
  FileText,
  FlaskConical,
  GitCommit,
  GitPullRequest,
  GraduationCap,
  Highlighter,
  Languages,
  LayoutDashboard,
  Lightbulb,
  ListChecks,
  Megaphone,
  MessageSquare,
  Microscope,
  Palette,
  PenLine,
  Presentation,
  Scale,
  Search,
  Sparkles,
  Target,
  Type,
  UserRound,
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
