"use client";

import type { ReactNode } from "react";

import type { Rock, VisionPlanSection } from "../core/types";

// The Vision/Traction organiser has a fixed shape: four labelled rows plus the
// 3-Year Picture on Vision, three columns on Traction. Sections are matched by
// their stable key; the heading still shows the workspace's own section name.
export const VISION_ROW_KEYS = ["core-values", "core-focus", "long-term-target", "marketing-strategy"] as const;
export const THREE_YEAR_KEY = "three-year-picture";
export const ONE_YEAR_KEY = "one-year-plan";
export const ISSUES_LIST_KEY = "issues-list";

const BOARD_KEYS = new Set<string>([...VISION_ROW_KEYS, THREE_YEAR_KEY, ONE_YEAR_KEY, ISSUES_LIST_KEY]);

// Shown only when the workspace has not renamed the section itself.
const WIREFRAME_LABEL: Record<string, string> = {
  "core-values": "Core Values",
  "core-focus": "Core Focus",
  "long-term-target": "10-Year Target",
  "marketing-strategy": "Marketing Strategy",
  "three-year-picture": "3-Year Picture",
  "one-year-plan": "1-Year Plan",
  "issues-list": "Issues List",
};

function label(section: VisionPlanSection): string {
  return section.name.trim() || WIREFRAME_LABEL[section.key] || section.key;
}

// Every section that is not one of the six V/TO slots — custom sections, Core
// Processes, Quarterly Goals. Surfaced rather than hidden.
export function extraSections(sections: VisionPlanSection[]): VisionPlanSection[] {
  return sections.filter((section) => !BOARD_KEYS.has(section.key));
}

function ColumnHeading({ children }: { children: ReactNode }) {
  return <h2 className="border-b bg-muted/60 px-4 py-2.5 text-center text-sm font-semibold uppercase tracking-wide">{children}</h2>;
}

export function VisionBoard({ sections, renderSection }: {
  sections: VisionPlanSection[];
  renderSection: (section: VisionPlanSection) => ReactNode;
}) {
  const rows = VISION_ROW_KEYS.map((key) => sections.find((section) => section.key === key)).filter((section): section is VisionPlanSection => Boolean(section));
  const picture = sections.find((section) => section.key === THREE_YEAR_KEY);

  return (
    <div className="flex flex-col gap-4 lg:flex-row lg:items-start">
      <div className="min-w-0 flex-1 overflow-hidden rounded-xl border bg-card">
        {rows.map((section, index) => (
          <div key={section.id} className={`grid lg:grid-cols-[190px_minmax(0,1fr)] ${index > 0 ? "border-t" : ""}`}>
            <div className="flex items-center bg-muted/60 px-4 py-3 lg:border-r">
              <h2 className="text-sm font-semibold uppercase tracking-wide">{label(section)}</h2>
            </div>
            <div className="min-w-0 p-3">{renderSection(section)}</div>
          </div>
        ))}
      </div>

      {picture && (
        <section aria-label={label(picture)} className="overflow-hidden rounded-xl border bg-card lg:w-80 lg:shrink-0">
          <ColumnHeading>{label(picture)}</ColumnHeading>
          <div className="p-3">{renderSection(picture)}</div>
        </section>
      )}
    </div>
  );
}

export function TractionBoard({ sections, rocks, rocksLabel, renderSection, onOpenRock }: {
  sections: VisionPlanSection[];
  rocks: Rock[];
  rocksLabel: string;
  renderSection: (section: VisionPlanSection) => ReactNode;
  onOpenRock: (rockId: string) => void;
}) {
  const oneYear = sections.find((section) => section.key === ONE_YEAR_KEY);
  const issues = sections.find((section) => section.key === ISSUES_LIST_KEY);

  return (
    <div className="grid gap-4 lg:grid-cols-3 lg:items-start">
      {oneYear && (
        <section aria-label={label(oneYear)} className="overflow-hidden rounded-xl border bg-card">
          <ColumnHeading>{label(oneYear)}</ColumnHeading>
          <div className="p-3">{renderSection(oneYear)}</div>
        </section>
      )}

      <section aria-label={rocksLabel} className="overflow-hidden rounded-xl border bg-card">
        <ColumnHeading>{rocksLabel}</ColumnHeading>
        <div className="p-3">
          {rocks.length === 0
            ? <p className="px-1 py-2 text-sm text-muted-foreground">No {rocksLabel.toLowerCase()} in the current period yet.</p>
            : <table className="w-full text-sm">
                <thead>
                  <tr className="text-left text-xs uppercase tracking-wide text-muted-foreground">
                    <th scope="col" className="px-1 pb-2 font-medium">{rocksLabel} for the quarter</th>
                    <th scope="col" className="px-1 pb-2 font-medium">Who</th>
                  </tr>
                </thead>
                <tbody>
                  {rocks.map((rock) => (
                    <tr key={rock.id} className="border-t align-top">
                      <td className="px-1 py-2">
                        <button type="button" onClick={() => onOpenRock(rock.id)} className="text-left hover:underline">{rock.title}</button>
                      </td>
                      <td className="px-1 py-2 text-muted-foreground">{rock.owner_name || "Unassigned"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>}
        </div>
      </section>

      {issues && (
        <section aria-label={label(issues)} className="overflow-hidden rounded-xl border bg-card">
          <ColumnHeading>{label(issues)}</ColumnHeading>
          <div className="p-3">{renderSection(issues)}</div>
        </section>
      )}
    </div>
  );
}
