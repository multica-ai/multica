"use client";

import {
  FileText,
  ListChecks,
  GitBranch,
  Workflow,
  StickyNote,
  type LucideIcon,
} from "lucide-react";
import type { ArtifactKind } from "@multica/core/types";

export const KIND_LABELS: Record<ArtifactKind, string> = {
  report: "Report",
  plan: "Plan",
  decision: "Decision",
  diagram: "Diagram",
  note: "Note",
};

const ICONS: Record<ArtifactKind, LucideIcon> = {
  report: FileText,
  plan: ListChecks,
  decision: GitBranch,
  diagram: Workflow,
  note: StickyNote,
};

export function KindIcon({
  kind,
  className,
}: {
  kind: ArtifactKind;
  className?: string;
}) {
  const Icon = ICONS[kind];
  return <Icon className={className} aria-hidden="true" />;
}
