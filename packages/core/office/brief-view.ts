import type { Autopilot } from "../types";
import type { Brief } from "./brief-schema";

export interface BriefSection {
  /** The H2 heading text, without the marker. */
  heading: string;
  /** The markdown under the heading, trimmed. */
  body: string;
}

/**
 * Splits a brief's markdown at its H2 headings, in order. Text before the first
 * heading is not a section; the whole brief still renders from its full body.
 */
export function splitBriefSections(markdown: string): BriefSection[] {
  const sections: { heading: string; lines: string[] }[] = [];
  for (const line of markdown.split("\n")) {
    const heading = /^##\s+(.+?)\s*$/.exec(line);
    if (heading) {
      sections.push({ heading: heading[1]!, lines: [] });
    } else {
      sections.at(-1)?.lines.push(line);
    }
  }
  return sections.map(({ heading, lines }) => ({ heading, body: lines.join("\n").trim() }));
}

/** Whether a brief was created on the viewer's local calendar day. */
export function isBriefToday(brief: Pick<Brief, "created_at">, now: Date = new Date()): boolean {
  const created = new Date(brief.created_at);
  if (Number.isNaN(created.getTime())) return false;
  return (
    created.getFullYear() === now.getFullYear() &&
    created.getMonth() === now.getMonth() &&
    created.getDate() === now.getDate()
  );
}

/** Whether the newest brief is not the one the viewer last opened. */
export function hasUnreadBrief(latestId: string | undefined, lastSeenId: string | null): boolean {
  return !!latestId && latestId !== lastSeenId;
}

/**
 * The Secretary's autopilot: the one assigned to the Secretary agent, preferring
 * an active one over a paused one. Archived autopilots never count.
 */
export function findSecretaryAutopilot(
  autopilots: readonly Autopilot[],
  secretaryId: string | null,
): Autopilot | null {
  if (!secretaryId) return null;
  const owned = autopilots.filter(
    (autopilot) =>
      autopilot.assignee_type === "agent" &&
      autopilot.assignee_id === secretaryId &&
      autopilot.status !== "archived",
  );
  return owned.find((autopilot) => autopilot.status === "active") ?? owned[0] ?? null;
}
