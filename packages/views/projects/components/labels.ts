"use client";

import type { ProjectStatus, ProjectPriority } from "@multica/core/types";
import { useT } from "../../i18n";

// Hooks returning the i18n-aware label maps for project status / priority.
// They replace the static `.label` field on PROJECT_STATUS_CONFIG /
// PROJECT_PRIORITY_CONFIG for view-layer rendering. Core's `.label` stays
// for non-translated callers (search, create-project modal) — those will
// flip when their namespaces translate. Mirror of inbox `useTypeLabels`.

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
