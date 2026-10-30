import { ArrowUpRight, Bot, CheckCircle2, GitCompare, Pin, Play } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  Breadcrumbs,
  PAGE_GUTTER,
  PAGE_RAIL,
  RevisionNumber,
  SkillIcon,
} from "./chrome";
import {
  SKILL_NAME,
  type MockAgent,
  type MockRun,
  type SkillRevision,
  type TaskSkillUse,
} from "./fixtures";

export function RunPage({
  runs,
  agents,
  revisions,
  onRun,
  onOpenRevision,
  onCompare,
  navigate,
}: {
  runs: MockRun[];
  agents: MockAgent[];
  revisions: SkillRevision[];
  onRun: (agentId: string) => void;
  onOpenRevision: (n: number) => void;
  onCompare: (from: number, to: number) => void;
  navigate: (path: string) => void;
}) {
  const first = runs[runs.length - 1]!;
  const current = revisions[revisions.length - 1]!;
  const agentName = (id: string) => agents.find((agent) => agent.id === id)?.name ?? id;
  return (
    <div className="flex h-full min-h-0 flex-col">
      <Breadcrumbs
        crumbs={[{ label: "Issues", href: "/ui-lab/issues" }]}
        leaf={<span className="truncate text-caption text-foreground">{first.issue}</span>}
        actions={
          <div className="flex items-center gap-1" role="group" aria-label="模拟运行">
            <span className="mr-1 text-caption text-muted-foreground">模拟再运行一次：</span>
            {agents.map((agent) => (
              <Button
                key={agent.id}
                size="xs"
                variant="ghost"
                className="gap-1"
                onClick={() => onRun(agent.id)}
              >
                <Play className="h-3 w-3" />
                {agent.name}
              </Button>
            ))}
          </div>
        }
        onNavigate={navigate}
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className={cn(PAGE_RAIL, PAGE_GUTTER, "max-w-4xl py-5")}>
          <h1 className="text-title-sm font-semibold">{first.issueTitle}</h1>
          <div className="mt-6 space-y-4">
            {runs.map((run) => (
              <article key={run.id} className="rounded-lg border bg-surface-raised/40">
                <header className="flex flex-wrap items-center gap-2 border-b px-4 py-2.5">
                  <span className="flex size-5 items-center justify-center rounded-full bg-muted">
                    <Bot className="size-3" aria-hidden="true" />
                  </span>
                  <span className="text-body font-medium">{agentName(run.agentId)}</span>
                  <span className="inline-flex items-center gap-1 text-caption text-success">
                    <CheckCircle2 className="size-3.5" aria-hidden="true" />
                    已完成
                  </span>
                  <span className="ml-auto text-caption tabular-nums text-muted-foreground">
                    {run.startedAt} · 用时 {run.duration}
                  </span>
                </header>
                <p className="px-4 pt-3 text-body text-muted-foreground">{run.summary}</p>
                <section className="px-4 pt-3 pb-4" aria-label="本次运行使用的 Skill">
                  <h3 className="mb-2 text-micro font-medium uppercase tracking-wider text-muted-foreground">
                    本次运行使用的 Skill
                  </h3>
                  <div className="overflow-hidden rounded-md border bg-background">
                    <table className="w-full text-caption">
                      <thead className="bg-muted/40 text-muted-foreground">
                        <tr className="[&>th]:px-3 [&>th]:py-1.5 [&>th]:text-left [&>th]:font-medium">
                          <th>Skill</th>
                          <th>版本</th>
                          <th>挂载方式</th>
                          <th>Hash</th>
                          <th className="text-right">
                            <span className="sr-only">操作</span>
                          </th>
                        </tr>
                      </thead>
                      <tbody className="divide-y">
                        {run.skills.map((use) => (
                          <SkillUseRow
                            key={use.skill}
                            use={use}
                            currentNumber={current.number}
                            onOpenRevision={onOpenRevision}
                            onCompare={onCompare}
                          />
                        ))}
                      </tbody>
                    </table>
                  </div>
                </section>
              </article>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function SkillUseRow({
  use,
  currentNumber,
  onOpenRevision,
  onCompare,
}: {
  use: TaskSkillUse;
  currentNumber: number;
  onOpenRevision: (n: number) => void;
  onCompare: (from: number, to: number) => void;
}) {
  const linkable = use.kind === "workspace" && use.skill === SKILL_NAME && !use.deleted;
  return (
    <tr className="[&>td]:px-3 [&>td]:py-2">
      <td>
        <span className="inline-flex items-center gap-1.5">
          <SkillIcon className="size-3.5 text-muted-foreground" aria-hidden="true" />
          <span
            className={cn(
              "font-mono",
              use.kind === "workspace" && use.deleted && "text-muted-foreground line-through",
            )}
          >
            {use.skill}
          </span>
        </span>
      </td>
      <td>
        {use.kind === "builtin" ? (
          <span className="text-muted-foreground">内置 · server {use.serverVersion}</span>
        ) : (
          <span className="inline-flex items-center gap-1.5">
            <RevisionNumber n={use.revision} className="font-medium" />
            {linkable &&
              (use.revision === currentNumber ? (
                <Badge variant="outline" className="h-4 px-1.5 text-micro">
                  当前
                </Badge>
              ) : (
                <span className="text-micro text-muted-foreground">
                  当前已是 #{currentNumber}
                </span>
              ))}
            {use.kind === "workspace" && use.deleted && (
              <span className="text-micro text-muted-foreground">skill 已删除</span>
            )}
          </span>
        )}
      </td>
      <td className="text-muted-foreground">
        {use.kind === "builtin" ? (
          "—"
        ) : use.binding === "pinned" ? (
          <span className="inline-flex items-center gap-1">
            <Pin className="size-3" aria-hidden="true" />
            固定版本
          </span>
        ) : (
          "跟随最新"
        )}
      </td>
      <td className="font-mono text-micro text-muted-foreground">{use.hash.slice(0, 19)}</td>
      <td className="text-right">
        {linkable && use.kind === "workspace" && (
          <span className="inline-flex items-center gap-1">
            <Button
              variant="ghost"
              size="xs"
              className="gap-1"
              onClick={() => onOpenRevision(use.revision)}
            >
              <ArrowUpRight className="h-3 w-3" />
              查看此版本
            </Button>
            {use.revision !== currentNumber ? (
              <Button
                variant="ghost"
                size="xs"
                className="gap-1"
                onClick={() => onCompare(use.revision, currentNumber)}
              >
                <GitCompare className="h-3 w-3" />
                与当前对比
              </Button>
            ) : use.revision > 1 ? (
              <Button
                variant="ghost"
                size="xs"
                className="gap-1"
                onClick={() => onCompare(use.revision - 1, use.revision)}
              >
                <GitCompare className="h-3 w-3" />
                与 #{use.revision - 1} 对比
              </Button>
            ) : null}
          </span>
        )}
      </td>
    </tr>
  );
}
