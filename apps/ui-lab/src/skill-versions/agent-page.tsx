import { Bot, Pin, Plus, Puzzle, Trash2 } from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import {
  Breadcrumbs,
  DetailTabs,
  PAGE_GUTTER,
  PAGE_RAIL,
  RevisionNumber,
  SkillIcon,
} from "./chrome";
import {
  SKILL_DESCRIPTION,
  SKILL_NAME,
  type Binding,
  type MockAgent,
  type SkillRevision,
} from "./fixtures";

const LATEST = "latest";

export function AgentPage({
  agent,
  agents,
  revisions,
  onSelectAgent,
  onBind,
  onCompare,
  navigate,
}: {
  agent: MockAgent;
  agents: MockAgent[];
  revisions: SkillRevision[];
  onSelectAgent: (id: string) => void;
  onBind: (agentId: string, binding: Binding) => void;
  onCompare: (from: number, to: number) => void;
  navigate: (path: string) => void;
}) {
  const current = revisions[revisions.length - 1]!;
  const behind = agent.binding !== null && agent.binding < current.number;
  const items = [
    { value: LATEST, label: `跟随最新 · #${current.number}` },
    ...[...revisions].reverse().map((revision) => ({
      value: String(revision.number),
      label: `固定 #${revision.number}`,
    })),
  ];
  return (
    <div className="flex h-full min-h-0 flex-col">
      <Breadcrumbs
        crumbs={[{ label: "Agents", href: "/ui-lab/agents" }]}
        leaf={<span className="truncate text-caption text-foreground">{agent.name}</span>}
        actions={
          <div className="flex items-center gap-1" role="group" aria-label="切换示例 agent">
            {agents.map((item) => (
              <Button
                key={item.id}
                size="xs"
                variant={item.id === agent.id ? "secondary" : "ghost"}
                onClick={() => onSelectAgent(item.id)}
              >
                {item.name}
              </Button>
            ))}
          </div>
        }
        onNavigate={navigate}
      />
      <div className="shrink-0 border-b py-3">
        <div className={cn(PAGE_RAIL, PAGE_GUTTER, "flex items-center gap-2.5")}>
          <div className="flex h-8 w-8 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
            <Bot className="h-4 w-4" aria-hidden="true" />
          </div>
          <h1 className="text-title font-semibold tracking-tight">{agent.name}</h1>
          <span className="text-caption text-muted-foreground">{agent.description}</span>
        </div>
      </div>
      <DetailTabs
        tabs={[
          { id: "overview", label: "概览" },
          { id: "instructions", label: "指令" },
          { id: "skills", label: "Skills" },
          { id: "tasks", label: "任务" },
        ]}
        active="skills"
        onSelect={() => {}}
        label="Agent 详情"
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className={cn(PAGE_RAIL, PAGE_GUTTER, "max-w-4xl space-y-8 py-5")}>
          <section>
            <div className="mb-3 flex items-start justify-between gap-4">
              <div>
                <h2 className="text-body font-medium">Workspace skills</h2>
                <p className="mt-0.5 text-caption text-muted-foreground">
                  每个 skill 可以跟随最新版本，或者固定在某个版本。
                </p>
              </div>
              <Button variant="outline" size="sm">
                <Plus className="h-3.5 w-3.5" />
                添加 skill
              </Button>
            </div>
            <ul className="divide-y rounded-lg border bg-surface-raised/40">
              <li className="flex items-center gap-3 p-3">
                <SkillGlyph />
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <span className="text-body font-medium">{SKILL_NAME}</span>
                    {behind && (
                      <Badge variant="outline" className="border-warning/40 text-warning">
                        有新版本 #{current.number}
                      </Badge>
                    )}
                  </span>
                  <span className="block truncate text-caption text-muted-foreground">
                    {behind ? (
                      <>
                        固定在 #{agent.binding}，最新是 #{current.number} ·{" "}
                        <button
                          type="button"
                          className="text-foreground underline-offset-2 hover:underline"
                          onClick={() => onCompare(agent.binding!, current.number)}
                        >
                          查看差异
                        </button>
                      </>
                    ) : (
                      SKILL_DESCRIPTION
                    )}
                  </span>
                </span>
                <Select
                  items={items}
                  value={agent.binding === null ? LATEST : String(agent.binding)}
                  onValueChange={(value) =>
                    value && onBind(agent.id, value === LATEST ? null : Number(value))
                  }
                >
                  <SelectTrigger
                    size="sm"
                    aria-label={`${SKILL_NAME} 使用的版本`}
                    className="min-w-36"
                  >
                    {agent.binding !== null && (
                      <Pin className="size-3.5 text-muted-foreground" aria-hidden="true" />
                    )}
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="min-w-64">
                    <SelectItem value={LATEST}>
                      <span className="flex flex-col">
                        <span>跟随最新 · #{current.number}</span>
                        <span className="text-micro text-muted-foreground">
                          保存新版本后，下一个任务自动使用
                        </span>
                      </span>
                    </SelectItem>
                    <SelectSeparator />
                    {[...revisions].reverse().map((revision) => (
                      <SelectItem key={revision.id} value={String(revision.number)}>
                        <span className="flex min-w-0 flex-col">
                          <span>
                            固定 <RevisionNumber n={revision.number} />
                            {revision.number === current.number ? "（当前）" : ""}
                          </span>
                          <span className="truncate text-micro text-muted-foreground">
                            {revision.note || "没有填写变更说明"} · {revision.createdAt}
                          </span>
                        </span>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Switch defaultChecked aria-label={`启用 ${SKILL_NAME}`} />
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={`移除 ${SKILL_NAME}`}
                  className="text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </li>
              <li className="flex items-center gap-3 p-3">
                <SkillGlyph />
                <span className="min-w-0 flex-1">
                  <span className="block text-body font-medium">release-notes</span>
                  <span className="block truncate text-caption text-muted-foreground">
                    根据合并的 PR 生成发布说明
                  </span>
                </span>
                <span className="inline-flex h-7 min-w-36 items-center rounded-md border px-2.5 text-body">
                  跟随最新 · #3
                </span>
                <Switch defaultChecked aria-label="启用 release-notes" />
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label="移除 release-notes"
                  className="text-muted-foreground hover:text-destructive"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              </li>
              <li className="flex items-center gap-3 p-3">
                <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
                  <Puzzle className="h-4 w-4" />
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <span className="text-body font-medium">jira-sync</span>
                    <Badge variant="secondary">插件</Badge>
                  </span>
                  <span className="block truncate text-caption text-muted-foreground">
                    版本跟随插件，不能单独固定或回滚
                  </span>
                </span>
                <span className="inline-flex h-7 min-w-36 items-center rounded-md px-2.5 font-mono text-body text-muted-foreground">
                  #2
                </span>
                <Switch defaultChecked aria-label="启用 jira-sync" />
                <span className="size-7" />
              </li>
            </ul>
            <p className="mt-2 text-caption text-muted-foreground">
              {agent.name} 的下一个任务会使用 {SKILL_NAME}{" "}
              <RevisionNumber n={agent.binding ?? current.number} />。
            </p>
          </section>
        </div>
      </div>
    </div>
  );
}

function SkillGlyph() {
  return (
    <span className="flex size-9 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
      <SkillIcon className="h-4 w-4" />
    </span>
  );
}
