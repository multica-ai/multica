"use client";

import type { ProjectStatus, ProjectPriority } from "@multica/core/types";
import { useT } from "../../i18n";

// Hooks returning the i18n-aware label maps for project status / priority.
// They replace the static `.label` field on PROJECT_STATUS_CONFIG /
// PROJECT_PRIORITY_CONFIG, and search — the last caller still rendering
// `PROJECT_STATUS_CONFIG.label` — now resolves through here too, so nothing
// in the repo reads either `.label` any more. Name a project status through
// these hooks; the core field is a leftover, not a second source of truth.
// Mirror of inbox `useTypeLabels`.

export function useProjectStatusLabels(): Record<ProjectStatus, string> {
  const { t } = useT("projects");
  return {
    planned: t(($) => $.status.planned),
    in_progress: t(($) => $.status.in_progress),
    paused: t(($) => $.status.paused),
    completed: t(($) => $.status.completed),
    cancelled: t(($) => $.status.cancelled),
  };
}

export function useProjectPriorityLabels(): Record<ProjectPriority, string> {
  const { t } = useT("projects");
  return {
    urgent: t(($) => $.priority.urgent),
    high: t(($) => $.priority.high),
    medium: t(($) => $.priority.medium),
    low: t(($) => $.priority.low),
    none: t(($) => $.priority.none),
  };
}

// "1d ago" / "3mo ago" / "Today" — relative date helper that flows through
// i18next. Returns a function so callers keep the previous
// `formatRelativeDate(iso)` shape.
export function useFormatRelativeDate(): (date: string) => string {
  const { t } = useT("projects");
  return (date: string) => {
    const diff = Date.now() - new Date(date).getTime();
    const days = Math.floor(diff / (1000 * 60 * 60 * 24));
    if (days < 1) return t(($) => $.relative_date.today);
    if (days === 1) return t(($) => $.relative_date.one_day_ago);
    if (days < 30) return t(($) => $.relative_date.days_ago, { count: days });
    const months = Math.floor(days / 30);
    return t(($) => $.relative_date.months_ago, { count: months });
  };
}
