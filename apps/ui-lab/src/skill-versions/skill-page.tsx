import { useMemo, useState } from "react";
import {
  AlertTriangle,
  Clock3,
  FileText,
  History,
  RotateCcw,
  Users,
} from "lucide-react";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { cn } from "@multica/ui/lib/utils";
import {
  ActorChip,
  Breadcrumbs,
  DetailTabs,
  PAGE_GUTTER,
  PAGE_RAIL,
  RevisionNumber,
  SkillIcon,
} from "./chrome";
import { diffSkill, type FileDiff, type FileStatus } from "./diff";
import {
  SKILL_DESCRIPTION,
  SKILL_NAME,
  type MockAgent,
  type RevisionSource,
  type SkillRevision,
} from "./fixtures";

export type SaveResult = {
  status: "saved" | "conflict" | "unchanged";
  number: number;
};

export type SkillTab = "overview" | "files" | "history";

export type SkillPageState = {
  tab: SkillTab;
  selected: number;
  from: number;
  to: number;
};

const SOURCE_LABEL: Record<RevisionSource, string> = {
  create: "创建",
  edit: "编辑",
  import: "导入",
  refresh: "从来源刷新",
  plugin: "插件",
  restore: "回滚",
  backfill: "初始版本",
};

const STATUS_LABEL: Record<FileStatus, string> = {
  added: "新增",
  removed: "删除",
  modified: "修改",
  unchanged: "未变化",
};

const STATUS_CLASS: Record<FileStatus, string> = {
  added: "bg-success/10 text-success",
  removed: "bg-destructive/10 text-destructive",
  modified: "bg-info/10 text-info",
  unchanged: "bg-muted text-muted-foreground",
};

export function SkillPage({
  revisions,
  agents,
  view,
  onView,
  onRestore,
  onSave,
  navigate,
}: {
  revisions: SkillRevision[];
  agents: MockAgent[];
  view: SkillPageState;
  onView: (next: Partial<SkillPageState>) => void;
  onRestore: (n: number, note: string) => void;
  onSave: (content: string, note: string, overwrite: boolean) => SaveResult;
  navigate: (path: string) => void;
}) {
  const current = revisions[revisions.length - 1]!;
  const fileCount = Object.keys(current.files).length;
  const tabs: { id: SkillTab; label: string }[] = [
    { id: "overview", label: "概览" },
    { id: "files", label: `文件 (${fileCount})` },
    { id: "history", label: `版本历史 (${revisions.length})` },
  ];
  return (
    <div className="flex h-full min-h-0 flex-col">
      <Breadcrumbs
        crumbs={[{ label: "Skills", href: "/ui-lab/skills" }]}
        leaf={
          <span className="truncate font-mono text-caption text-foreground">
            {SKILL_NAME}
          </span>
        }
        actions={
          <Button
            variant="outline"
            size="xs"
            className="gap-1"
            onClick={() => onView({ tab: "history" })}
          >
            <History className="h-3 w-3" />
            版本历史
          </Button>
        }
        onNavigate={navigate}
      />
      <div className="shrink-0 border-b py-3">
        <div
          className={cn(
            PAGE_RAIL,
            PAGE_GUTTER,
            "flex flex-wrap items-center gap-x-4 gap-y-1.5",
          )}
        >
          <div className="flex min-w-0 items-center gap-2.5">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border bg-muted text-muted-foreground">
              <SkillIcon className="h-4 w-4" aria-hidden="true" />
            </div>
            <h1 className="min-w-0 truncate font-mono text-title font-semibold tracking-tight">
              {SKILL_NAME}
            </h1>
            <Badge variant="outline" className="font-mono">
              当前 #{current.number}
            </Badge>
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-x-4 gap-y-1 text-caption text-muted-foreground sm:ml-auto">
            <span className="inline-flex items-center gap-1.5">
              <FileText className="h-3.5 w-3.5" aria-hidden="true" />
              {fileCount} 个文件
            </span>
            <button
              type="button"
              onClick={() => navigate("/ui-lab/agents")}
              className="inline-flex items-center gap-1.5 hover:text-foreground hover:underline"
            >
              <Users className="h-3.5 w-3.5" aria-hidden="true" />
              {agents.length} 个 agent 使用
            </button>
            <span className="inline-flex items-center gap-1.5">
              <Clock3 className="h-3.5 w-3.5" aria-hidden="true" />
              {current.author.name} 更新于 {current.createdAt}
            </span>
          </div>
        </div>
      </div>
      <DetailTabs
        tabs={tabs}
        active={view.tab}
        onSelect={(tab) => onView({ tab })}
        label="Skill 详情"
      />
      <div className="min-h-0 flex-1 overflow-y-auto">
        {view.tab === "history" ? (
          <HistoryTab
            revisions={revisions}
            agents={agents}
            view={view}
            onView={onView}
            onRestore={onRestore}
          />
        ) : view.tab === "files" ? (
          <FilesTab
            current={current}
            onSave={onSave}
            onShowConflict={(from, to) =>
              onView({ tab: "history", from, to, selected: to })
            }
          />
        ) : (
          <OverviewTab
            current={current}
            agents={agents}
            onHistory={() => onView({ tab: "history" })}
          />
        )}
      </div>
    </div>
  );
}

function OverviewTab({
  current,
  agents,
  onHistory,
}: {
  current: SkillRevision;
  agents: MockAgent[];
  onHistory: () => void;
}) {
  const pinned = agents.filter((agent) => agent.binding !== null);
  return (
    <div className={cn(PAGE_RAIL, PAGE_GUTTER, "max-w-3xl py-4")}>
      <PropertyRow label="描述">{SKILL_DESCRIPTION}</PropertyRow>
      <PropertyRow label="当前版本">
        <span className="inline-flex flex-wrap items-center gap-x-2 gap-y-1">
          <RevisionNumber n={current.number} className="font-medium" />
          <span className="text-muted-foreground">
            {current.author.name} · {current.createdAt}
            {current.note ? ` · ${current.note}` : ""}
          </span>
          <Button variant="link" size="xs" className="h-auto px-0" onClick={onHistory}>
            查看版本历史
          </Button>
        </span>
      </PropertyRow>
      <PropertyRow label="使用方式">
        {agents.length - pinned.length} 个 agent 跟随最新
        {pinned.length > 0
          ? `，${pinned.length} 个固定版本（${pinned
              .map((agent) => `${agent.name} #${agent.binding}`)
              .join("、")}）`
          : ""}
      </PropertyRow>
    </div>
  );
}

function PropertyRow({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid gap-1.5 py-2.5 sm:grid-cols-[128px_minmax(0,1fr)] sm:gap-4">
      <div className="pt-0.5 text-caption text-muted-foreground sm:text-body">
        {label}
      </div>
      <div className="min-w-0 text-body">{children}</div>
    </div>
  );
}

function HistoryTab({
  revisions,
  agents,
  view,
  onView,
  onRestore,
}: {
  revisions: SkillRevision[];
  agents: MockAgent[];
  view: SkillPageState;
  onView: (next: Partial<SkillPageState>) => void;
  onRestore: (n: number, note: string) => void;
}) {
  const current = revisions[revisions.length - 1]!;
  const byNumber = (n: number) =>
    revisions.find((revision) => revision.number === n) ?? current;
  const from = byNumber(view.from);
  const to = byNumber(view.to);
  const diff = useMemo(() => diffSkill(from.files, to.files), [from, to]);
  const changed = diff.filter((file) => file.status !== "unchanged");
  const unchanged = diff.filter((file) => file.status === "unchanged");
  const additions = changed.reduce((sum, file) => sum + file.additions, 0);
  const deletions = changed.reduce((sum, file) => sum + file.deletions, 0);
  const [restoreTarget, setRestoreTarget] = useState<number | null>(null);
  const items = revisions.map((revision) => ({
    value: String(revision.number),
    label: `#${revision.number}`,
  }));

  return (
    <div
      className={cn(
        PAGE_RAIL,
        PAGE_GUTTER,
        "grid gap-6 py-4 lg:grid-cols-[300px_minmax(0,1fr)]",
      )}
    >
      <aside className="min-w-0">
        <div className="mb-2 flex items-baseline justify-between">
          <h2 className="text-body font-medium">{revisions.length} 个版本</h2>
          <span className="text-caption text-muted-foreground">
            每次保存生成一个新版本
          </span>
        </div>
        <ul
          className="divide-y overflow-hidden rounded-lg border bg-surface-raised/40"
          role="listbox"
          aria-label="版本列表"
        >
          {[...revisions].reverse().map((revision) => {
            const selected = revision.number === view.selected;
            const isCurrent = revision.number === current.number;
            const pinnedHere = agents.filter(
              (agent) => agent.binding === revision.number,
            );
            return (
              <li key={revision.id}>
                <button
                  type="button"
                  role="option"
                  aria-selected={selected}
                  onClick={() =>
                    onView({
                      selected: revision.number,
                      to: revision.number,
                      from: Math.max(1, revision.number - 1),
                    })
                  }
                  className={cn(
                    "block w-full px-3 py-2.5 text-left outline-none transition-colors focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring",
                    selected
                      ? "bg-surface-selected text-surface-selected-foreground"
                      : "hover:bg-surface-hover",
                  )}
                >
                  <div className="flex items-center gap-1.5">
                    <RevisionNumber n={revision.number} className="text-body font-semibold" />
                    {isCurrent && <Badge className="h-4 px-1.5 text-micro">当前</Badge>}
                    <span className="rounded-sm bg-muted px-1.5 text-micro text-muted-foreground">
                      {revision.source === "restore"
                        ? `回滚自 #${revision.restoredFrom}`
                        : SOURCE_LABEL[revision.source]}
                    </span>
                    <span className="ml-auto shrink-0 text-micro tabular-nums text-muted-foreground">
                      {revision.createdAt}
                    </span>
                  </div>
                  <div
                    className={cn(
                      "mt-1 truncate text-caption",
                      revision.note ? "text-foreground" : "text-faint-foreground",
                    )}
                  >
                    {revision.note || "没有填写变更说明"}
                  </div>
                  <div className="mt-1 flex items-center gap-2 text-micro text-muted-foreground">
                    <ActorChip type={revision.author.type} name={revision.author.name} />
                    {pinnedHere.length > 0 && (
                      <span className="ml-auto shrink-0">
                        {pinnedHere.map((agent) => agent.name).join("、")} 固定在此版本
                      </span>
                    )}
                  </div>
                </button>
              </li>
            );
          })}
        </ul>
      </aside>

      <section className="min-w-0" aria-label="版本对比">
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <span className="text-caption text-muted-foreground">对比</span>
          <Select
            items={items}
            value={String(view.from)}
            onValueChange={(value) => value && onView({ from: Number(value) })}
          >
            <SelectTrigger size="sm" aria-label="旧版本" className="font-mono">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {items.map((item) => (
                <SelectItem key={item.value} value={item.value} className="font-mono">
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-muted-foreground">→</span>
          <Select
            items={items}
            value={String(view.to)}
            onValueChange={(value) =>
              value && onView({ to: Number(value), selected: Number(value) })
            }
          >
            <SelectTrigger size="sm" aria-label="新版本" className="font-mono">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {items.map((item) => (
                <SelectItem key={item.value} value={item.value} className="font-mono">
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <span className="text-caption text-muted-foreground">
            {changed.length === 0 ? (
              "两个版本内容相同"
            ) : (
              <>
                {changed.length} 个文件有变化 ·{" "}
                <span className="text-success">+{additions}</span>{" "}
                <span className="text-destructive">−{deletions}</span>
              </>
            )}
          </span>
          <div className="ml-auto flex items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              className="gap-1"
              disabled={view.selected === current.number}
              onClick={() => setRestoreTarget(view.selected)}
            >
              <RotateCcw className="h-3.5 w-3.5" />
              回滚到 #{view.selected}
            </Button>
          </div>
        </div>

        <div className="space-y-3">
          {changed.map((file) => (
            <FileDiffCard key={file.path} file={file} />
          ))}
          {unchanged.length > 0 && (
            <div className="rounded-lg border border-dashed px-3 py-2 text-caption text-muted-foreground">
              {unchanged.length} 个文件未变化：
              <span className="font-mono">
                {unchanged.map((file) => file.path).join("、")}
              </span>
            </div>
          )}
        </div>
      </section>

      <RestoreDialog
        target={restoreTarget}
        current={current}
        revision={restoreTarget === null ? null : byNumber(restoreTarget)}
        agents={agents}
        onCancel={() => setRestoreTarget(null)}
        onConfirm={(note) => {
          if (restoreTarget !== null) onRestore(restoreTarget, note);
          setRestoreTarget(null);
        }}
      />
    </div>
  );
}

function FileDiffCard({ file }: { file: FileDiff }) {
  return (
    <div className="overflow-hidden rounded-lg border">
      <div className="flex items-center gap-2 border-b bg-muted/40 px-3 py-2">
        <FileText className="h-3.5 w-3.5 text-muted-foreground" aria-hidden="true" />
        <span className="min-w-0 truncate font-mono text-caption font-medium">
          {file.path}
        </span>
        <span
          className={cn(
            "rounded-sm px-1.5 text-micro font-medium",
            STATUS_CLASS[file.status],
          )}
        >
          {STATUS_LABEL[file.status]}
        </span>
        <span className="ml-auto font-mono text-micro tabular-nums">
          <span className="text-success">+{file.additions}</span>{" "}
          <span className="text-destructive">−{file.deletions}</span>
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full border-collapse font-mono text-caption leading-5">
          <tbody>
            {file.lines.map((line, index) => (
              <tr
                key={index}
                className={cn(
                  line.kind === "add" && "bg-success/10",
                  line.kind === "del" && "bg-destructive/10",
                )}
              >
                <td className="w-10 select-none border-r px-2 text-right text-faint-foreground tabular-nums">
                  {line.oldNo ?? ""}
                </td>
                <td className="w-10 select-none border-r px-2 text-right text-faint-foreground tabular-nums">
                  {line.newNo ?? ""}
                </td>
                <td
                  className={cn(
                    "w-5 select-none text-center",
                    line.kind === "add" && "text-success",
                    line.kind === "del" && "text-destructive",
                  )}
                >
                  {line.kind === "add" ? "+" : line.kind === "del" ? "−" : ""}
                </td>
                <td className="whitespace-pre pr-3">{line.text || " "}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function RestoreDialog({
  target,
  current,
  revision,
  agents,
  onCancel,
  onConfirm,
}: {
  target: number | null;
  current: SkillRevision;
  revision: SkillRevision | null;
  agents: MockAgent[];
  onCancel: () => void;
  onConfirm: (note: string) => void;
}) {
  const [note, setNote] = useState("");
  const following = agents.filter((agent) => agent.binding === null);
  const pinned = agents.filter((agent) => agent.binding !== null);
  const next = current.number + 1;
  return (
    <Dialog
      open={target !== null}
      onOpenChange={(open) => {
        if (!open) onCancel();
        else setNote("");
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>回滚到 #{target}？</DialogTitle>
          <DialogDescription>
            会用 #{target} 的内容创建新版本 #{next}。#{current.number} 和更早的版本都会保留，之后还可以再回滚。
          </DialogDescription>
        </DialogHeader>
        {revision && (
          <div className="rounded-lg border bg-muted/30 px-3 py-2 text-caption">
            <div className="flex items-center gap-1.5">
              <RevisionNumber n={revision.number} className="font-medium" />
              <span className="truncate">{revision.note || "没有填写变更说明"}</span>
            </div>
            <div className="mt-0.5 text-muted-foreground">
              {revision.author.name} · {revision.createdAt}
            </div>
          </div>
        )}
        <div className="space-y-1.5 text-caption">
          <div>
            <span className="font-medium">{following.length} 个 agent 跟随最新</span>
            <span className="text-muted-foreground">
              ，下一个任务开始使用 #{next}：{following.map((agent) => agent.name).join("、")}
            </span>
          </div>
          {pinned.length > 0 && (
            <div>
              <span className="font-medium">{pinned.length} 个 agent 固定了版本</span>
              <span className="text-muted-foreground">
                ，不受影响：
                {pinned.map((agent) => `${agent.name}（#${agent.binding}）`).join("、")}
              </span>
            </div>
          )}
        </div>
        <div className="space-y-1.5">
          <label htmlFor="restore-note" className="text-caption font-medium">
            变更说明（可选）
          </label>
          <Textarea
            id="restore-note"
            rows={2}
            value={note}
            placeholder={`回滚到 #${target}`}
            onChange={(event) => setNote(event.target.value)}
          />
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={onCancel}>
            取消
          </Button>
          <Button onClick={() => onConfirm(note || `回滚到 #${target}`)}>
            <RotateCcw className="h-3.5 w-3.5" />
            回滚
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function FilesTab({
  current,
  onSave,
  onShowConflict,
}: {
  current: SkillRevision;
  onSave: (content: string, note: string, overwrite: boolean) => SaveResult;
  onShowConflict: (from: number, to: number) => void;
}) {
  const [draft, setDraft] = useState(current.files["SKILL.md"] ?? "");
  const [note, setNote] = useState("");
  const [conflict, setConflict] = useState<number | null>(null);
  const [base, setBase] = useState(current.number);
  const save = (overwrite: boolean) => {
    const result = onSave(draft, note, overwrite);
    if (result.status === "conflict") {
      setConflict(result.number);
      return;
    }
    setConflict(null);
    setBase(result.number);
    if (result.status === "saved") setNote("");
  };
  const paths = Object.keys(current.files).sort();
  return (
    <>
      {conflict !== null && (
        <div
          role="status"
          aria-live="polite"
          className="shrink-0 border-b border-warning/30 bg-warning/10 py-2 text-caption"
        >
          <div className={cn(PAGE_RAIL, PAGE_GUTTER, "flex items-start gap-2")}>
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
            <div className="flex-1">
              Jiayuan 在你编辑期间把 code-review 更新到了 #{conflict}，你的修改基于 #{base}，还没有保存。
            </div>
            <Button
              variant="outline"
              size="xs"
              onClick={() => onShowConflict(base, conflict)}
            >
              查看 #{base} → #{conflict} 的差异
            </Button>
            <Button size="xs" onClick={() => save(true)}>
              仍然保存为新版本
            </Button>
          </div>
        </div>
      )}
      <div
        className={cn(
          PAGE_RAIL,
          PAGE_GUTTER,
          "grid gap-4 py-4 lg:grid-cols-[220px_minmax(0,1fr)]",
        )}
      >
        <ul className="space-y-0.5 text-caption">
          {paths.map((path) => (
            <li
              key={path}
              className={cn(
                "truncate rounded-md px-2 py-1 font-mono",
                path === "SKILL.md"
                  ? "bg-surface-selected text-surface-selected-foreground"
                  : "text-muted-foreground",
              )}
            >
              {path}
            </li>
          ))}
        </ul>
        <div className="min-w-0 space-y-2">
          <Textarea
            aria-label="SKILL.md"
            className="min-h-64 font-mono text-caption"
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
          />
          <div className="flex items-center gap-2">
            <Input
              aria-label="变更说明"
              placeholder="变更说明（可选），会显示在版本历史里"
              value={note}
              onChange={(event) => setNote(event.target.value)}
              className="h-8 flex-1"
            />
            <span className="text-caption text-muted-foreground">
              基于 #{base}
            </span>
            <Button size="sm" onClick={() => save(false)}>
              保存
            </Button>
          </div>
        </div>
      </div>
    </>
  );
}
