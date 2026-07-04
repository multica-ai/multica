"use client";

import type { Agent } from "@multica/core/types";

/**
 * Skills tab for the redesigned agent page. Read-only chip list of the skills
 * attached to the agent, mirroring the mockup's "Skills" tab. Editing still
 * happens on the current agent page (Skills tab) — this preview surface is a
 * clean, scannable overview. `agent.skills` is the same `AgentSkillSummary[]`
 * the list endpoint already returns (id / name / description).
 */
export function RedesignSkillsPanel({ agent }: { agent: Agent }) {
  const skills = agent.skills ?? [];

  return (
    <div className="p-5 md:p-6">
      <div className="mb-1 flex items-baseline gap-2.5">
        <h2 className="text-lg font-bold tracking-tight">Skills</h2>
        <span className="font-mono text-xs text-muted-foreground">
          {skills.length}
        </span>
      </div>
      <p className="mb-5 max-w-[660px] text-sm leading-relaxed text-muted-foreground">
        Skills loaded for this agent at runtime. Attach or detach skills from
        the agent's Skills tab.
      </p>

      {skills.length === 0 ? (
        <p className="text-sm text-muted-foreground">No skills attached yet.</p>
      ) : (
        <div className="flex flex-wrap gap-2">
          {skills.map((skill) => (
            <span
              key={skill.id}
              title={skill.description || undefined}
              className="rounded-md border bg-background px-2.5 py-1 font-mono text-xs text-foreground/80"
            >
              {skill.name}
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
