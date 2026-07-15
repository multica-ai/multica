export interface EffectiveHookRow { policy: string; version: number; source: string; reason: string; }

export function EffectiveHooks({ rows }: { rows: EffectiveHookRow[] }) {
  return <section aria-label="Effective hooks" className="p-6"><h2 className="text-lg font-semibold">Effective hooks</h2><p className="mb-4 text-sm text-muted-foreground">Policies are ordered from workspace to the narrowest matching scope.</p><div className="grid gap-2">{rows.map((row) => <article key={`${row.policy}-${row.version}`} className="rounded-lg border p-3"><div className="flex justify-between"><strong>{row.policy}</strong><span className="text-xs">v{row.version}</span></div><p className="text-sm text-muted-foreground">{row.source} · {row.reason}</p></article>)}</div></section>;
}
