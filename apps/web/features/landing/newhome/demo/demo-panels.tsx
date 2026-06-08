"use client";

// Lightweight panels for the Agents / Skills tabs in the demo browser. These
// are bespoke presentational views over the same mock data — not the heavy
// product pages — so the tabs feel real without extra coupling or risk.

import { AGENTS, ISSUES, RUNNING_TASKS, SKILLS } from "./mock-data";

export function AgentsPanel() {
  return (
    <div className="h-full overflow-auto px-5 py-5 [scrollbar-width:thin]">
      <div className="mx-auto grid max-w-[760px] grid-cols-1 gap-2.5 sm:grid-cols-2">
        {AGENTS.map((a) => {
          const task = RUNNING_TASKS.find((t) => t.agent_id === a.id);
          const issue = task && ISSUES.find((i) => i.id === task.issue_id);
          return (
            <div
              key={a.id}
              className="flex items-center gap-3 rounded-[6px] border border-[#0a0d12]/8 bg-white px-3.5 py-3"
            >
              {a.avatar_url ? (
                // eslint-disable-next-line @next/next/no-img-element
                <img src={a.avatar_url} alt="" className="size-8 shrink-0 rounded-full" />
              ) : (
                <span className="size-8 shrink-0 rounded-full bg-[#0a0d12]/10" />
              )}
              <div className="min-w-0">
                <div className="truncate text-[14px] font-semibold text-[#0a0d12]">
                  {a.name}
                </div>
                {issue ? (
                  <div className="flex items-center gap-1.5 truncate text-[12.5px] text-[#0a0d12]/55">
                    <span className="inline-block size-1.5 shrink-0 rounded-full bg-emerald-500" />
                    Working on {issue.identifier}
                  </div>
                ) : (
                  <div className="text-[12.5px] text-[#0a0d12]/45">Idle</div>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

export function SkillsPanel() {
  return (
    <div className="h-full overflow-auto px-5 py-5 [scrollbar-width:thin]">
      <div className="mx-auto grid max-w-[760px] grid-cols-1 gap-2.5 sm:grid-cols-2">
        {SKILLS.map((s) => (
          <div
            key={s.name}
            className="rounded-[6px] border border-[#0a0d12]/8 bg-white px-3.5 py-3"
          >
            <div className="text-[14px] font-semibold text-[#0a0d12]">{s.name}</div>
            <div className="mt-0.5 text-[12.5px] leading-5 text-[#0a0d12]/55">
              {s.description}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
