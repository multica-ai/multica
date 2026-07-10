import type { SkillUsageRow } from "./api";

export function SkillUsagePanel({
  rows,
  isLoading,
  onSelect,
}: {
  rows: SkillUsageRow[];
  isLoading: boolean;
  onSelect: (skill: string, mode: "include" | "exclude") => void;
}) {
  return (
    <section aria-label="Skill usage" className="overflow-hidden rounded-lg border bg-card">
      <header className="flex items-center justify-between border-b px-4 py-3">
        <div>
          <h2 className="text-sm font-medium">Skill usage</h2>
          <p className="text-xs text-muted-foreground">Runtime-reported invocations only</p>
        </div>
        <span className="font-mono text-xs text-muted-foreground">{rows.length} skills</span>
      </header>
      {isLoading ? (
        <p className="px-4 py-6 text-sm text-muted-foreground">Loading skill usage…</p>
      ) : rows.length === 0 ? (
        <p className="px-4 py-6 text-sm text-muted-foreground">
          No runtime-reported skill usage in this period.
        </p>
      ) : (
        <div className="divide-y">
          {rows.map((row) => (
            <div key={row.skill_id ?? row.skill_name} className="grid grid-cols-[minmax(0,1fr)_auto_auto] items-center gap-4 px-4 py-2.5">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{row.skill_name}</p>
                <p className="text-xs text-muted-foreground">{row.run_count} runs</p>
              </div>
              <span className="font-mono text-sm tabular-nums">{row.invocation_count}</span>
              <div className="flex gap-1">
                <button type="button" aria-label={`Include ${row.skill_name}`} onClick={() => onSelect(row.skill_name, "include")} className="rounded border px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                  Include
                </button>
                <button type="button" aria-label={`Exclude ${row.skill_name}`} onClick={() => onSelect(row.skill_name, "exclude")} className="rounded border border-dashed px-2 py-1 text-xs text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                  Exclude
                </button>
              </div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
