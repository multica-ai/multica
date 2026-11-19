import type { ReactNode } from "react";
import {
  ArrowUpRight, Bell, Box, Calendar, CheckCircle2, ChevronDown, ChevronRight,
  FileText, GitPullRequest, MoreHorizontal, PanelRight, Plus, Tag,
} from "@multica/ui/icons";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { cardStyle, type Treatment } from "./treatments";

type Status = "todo" | "in_progress" | "in_review" | "done";

/** Same drawing as packages/views/issues/components/status-icon.tsx. */
export function StatusIcon({ status, className }: { status: Status; className?: string }) {
  const color = status === "done" ? "text-info" : status === "todo" ? "text-muted-foreground" : "text-warning";
  return (
    <svg viewBox="0 0 14 14" className={cn("size-3.5 shrink-0", color, className)} aria-hidden="true">
      <circle cx="7" cy="7" r="6" fill="none" stroke="currentColor" strokeWidth="1.5" />
      {status === "in_progress" && <path d="M7,7 L7,3.5 A3.5,3.5 0 0,1 7,10.5 Z" fill="currentColor" />}
      {status === "in_review" && <path d="M7,7 L7,3.5 A3.5,3.5 0 1,1 3.5,7 Z" fill="currentColor" />}
      {status === "done" && (
        <>
          <circle cx="7" cy="7" r="6" fill="currentColor" />
          <path d="M10.95 4.25a.85.85 0 0 1 0 1.2l-5 5a.85.85 0 0 1-1.2 0l-2-2a.85.85 0 0 1 1.2-1.2l1.4 1.4 4.4-4.4a.85.85 0 0 1 1.2 0Z" fill="var(--surface)" />
        </>
      )}
    </svg>
  );
}

function PriorityHigh() {
  return (
    <svg viewBox="0 0 16 16" className="size-3.5 shrink-0 text-priority-high" aria-hidden="true">
      {[0, 1, 2, 3].map((i) => (
        <rect key={i} x={1.5 + i * 3.5} y={12 - (i + 1) * 2.6} width="2.4" height={(i + 1) * 2.6} rx="0.8" fill="currentColor" opacity={i > 2 ? 0.25 : 1} />
      ))}
    </svg>
  );
}

const Lambda = () => <ActorAvatar name="Lambda" initials="L" isAgent size="xs" className="bg-brand text-brand-foreground" />;

function LabelChip({ children, dot }: { children: ReactNode; dot: string }) {
  return (
    <span className="inline-flex h-6 items-center gap-1.5 rounded-full border px-2.5 text-label">
      <span className="size-1.5 rounded-full" style={{ background: dot }} />
      {children}
    </span>
  );
}

export function Card({ t, children, className }: { t: Treatment; children: ReactNode; className?: string }) {
  return (
    <section className={cn("pb-1.5", className)} style={cardStyle(t)}>
      {children}
    </section>
  );
}

function CardTitle({ children, right }: { children: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex h-10 items-center gap-1 px-4 pt-1 text-label font-medium text-muted-foreground">
      {children}
      <ChevronDown aria-hidden="true" className="size-3 text-faint-foreground" />
      {right && <span className="ml-auto flex items-center gap-1.5 text-caption font-normal">{right}</span>}
    </div>
  );
}

function Rows({ children }: { children: ReactNode }) {
  return <div className="px-2">{children}</div>;
}

function Row({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div className={cn("flex min-h-[34px] min-w-0 items-center gap-2 rounded-md px-2 text-label transition-colors hover:bg-accent/60", className)}>
      {children}
    </div>
  );
}

export function TopCard({ t }: { t: Treatment }) {
  return (
    <Card t={t} className="pt-1.5 pb-3.5">
      <Rows>
        <Row><StatusIcon status="in_review" /><span className="font-medium">审核中</span></Row>
        <Row><Lambda />Lambda</Row>
      </Rows>
      <div className="px-4 pt-2.5">
        <div className="flex items-center gap-2 text-label font-semibold">
          <GitPullRequest className="size-3.5 text-brand" />等你审阅 PR #4182
        </div>
        <div className="pl-[22px] text-caption text-muted-foreground">12/12 检查通过 · 可合入 · 8 分钟前</div>
        <div className="mt-2.5 flex gap-1.5 pl-[22px]">
          <Button size="sm">审阅 PR</Button>
          <Button size="sm" variant="outline">合入并关闭</Button>
        </div>
      </div>
    </Card>
  );
}

export function PropertiesCard({ t }: { t: Treatment }) {
  return (
    <Card t={t}>
      <CardTitle>属性</CardTitle>
      <Rows>
        <Row><Box className="size-3.5 text-muted-foreground" />Web 客户端</Row>
        <Row><PriorityHigh />高优先级</Row>
        <Row><Calendar className="size-3.5 text-muted-foreground" />11月21日截止</Row>
        <Row><Tag className="size-3.5 text-muted-foreground" /><LabelChip dot="var(--destructive)">Bug</LabelChip><LabelChip dot="var(--info)">收件箱</LabelChip></Row>
        <Row className="text-muted-foreground"><Plus className="size-3.5" />添加字段</Row>
      </Rows>
    </Card>
  );
}

export function OutputCard({ t }: { t: Treatment }) {
  return (
    <Card t={t}>
      <CardTitle right={<Plus className="size-3.5" />}>产出 <span className="font-normal tabular-nums">4</span></CardTitle>
      <Rows>
        <Row className="items-start py-2">
          <GitPullRequest className="mt-0.5 size-3.5 text-success" />
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 gap-1.5"><span className="text-muted-foreground">#4182</span><span className="truncate">fix(inbox): 切换工作区时重置未读计数</span></div>
            <div className="flex items-center gap-1.5 text-caption text-muted-foreground">
              <CheckCircle2 className="size-3 text-success" />12/12 检查通过 · <span className="text-success">可合入</span>
            </div>
          </div>
        </Row>
        <Row><FileText className="size-3.5 text-muted-foreground" /><span className="flex-1 truncate">inbox-before-after.png</span><span className="rounded-xs bg-muted px-1 text-micro text-muted-foreground">v2</span></Row>
        <Row><span className="mx-1 size-1.5 rounded-full bg-success" />web-preview<span className="flex-1 truncate text-caption text-muted-foreground">运行中 · 2 小时后过期</span><ArrowUpRight className="size-3.5 text-muted-foreground" /></Row>
      </Rows>
    </Card>
  );
}

export function RelationsCard({ t }: { t: Treatment }) {
  const subs: [Status, string, string][] = [
    ["done", "MUL-9418", "补充切换工作区的 e2e 用例"],
    ["done", "MUL-9419", "未读计数改为按工作区缓存"],
    ["in_progress", "MUL-9420", "桌面端多窗口之间同步未读计数"],
  ];
  return (
    <Card t={t}>
      <CardTitle right={<span className="tabular-nums">子任务 2/3</span>}>关联</CardTitle>
      <Rows>
        <Row><span className="text-caption text-muted-foreground">属于</span><StatusIcon status="in_progress" /><span className="text-muted-foreground">MUL-9380</span><span className="truncate">收件箱体验修复</span></Row>
        {subs.map(([s, id, title]) => (
          <Row key={id}><StatusIcon status={s} /><span className="shrink-0 text-muted-foreground">{id}</span><span className="truncate">{title}</span></Row>
        ))}
      </Rows>
    </Card>
  );
}

export function ExecutionCard({ t }: { t: Treatment }) {
  return (
    <Card t={t}>
      <CardTitle right={<span className="tabular-nums">4 次 · 38 分钟 · $1.84</span>}>执行</CardTitle>
      <Rows>
        <Row className="text-caption"><Lambda /><span className="flex-1 truncate text-muted-foreground">第 4 次运行完成 · 评论触发</span><span className="text-micro text-muted-foreground">8 分钟前</span></Row>
      </Rows>
    </Card>
  );
}

export function AutomationCard({ t }: { t: Treatment }) {
  return (
    <Card t={t} className="pb-0">
      <div className="flex h-10 items-center gap-1 px-4 text-label font-medium text-muted-foreground">
        自动化<ChevronRight aria-hidden="true" className="size-3 text-faint-foreground" />
        <span className="ml-auto text-caption font-normal">1 个唤醒 · 2 个快捷操作 · GitHub</span>
      </div>
    </Card>
  );
}

/** The full stack as it sits on the page canvas: no column, no divider. */
export function CardStack({ t, short = false }: { t: Treatment; short?: boolean }) {
  return (
    <div className="flex flex-col gap-2">
      <TopCard t={t} />
      <PropertiesCard t={t} />
      {!short && (
        <>
          <OutputCard t={t} />
          <RelationsCard t={t} />
          <ExecutionCard t={t} />
          <AutomationCard t={t} />
          <div className="px-4 pt-1 text-caption text-muted-foreground">Jiayuan 创建于 11月17日 · 元数据 3 项</div>
        </>
      )}
    </div>
  );
}

function Comment({ who, when, children }: { who: "Jiayuan" | "Lambda"; when: string; children: ReactNode }) {
  return (
    <div className="mt-3 rounded-xl border bg-card px-3 py-2.5">
      <div className="flex items-center gap-2 text-label">
        {who === "Lambda" ? <Lambda /> : <ActorAvatar name="Jiayuan" initials="J" size="xs" className="bg-priority-high text-brand-foreground" />}
        <span className="font-medium">{who}</span><span className="text-muted-foreground">{when}</span>
      </div>
      <div className="mt-1.5 text-body">{children}</div>
    </div>
  );
}

/** Issue page with the header spanning both columns and the cards floating on the canvas. */
export function IssuePage({ t }: { t: Treatment }) {
  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-page bg-page-canvas shadow-[var(--surface-shadow)] ring-1 ring-page-border">
      <header className="flex h-12 shrink-0 items-center gap-2 border-b px-5 text-body">
        <span className="flex items-center gap-1.5 text-muted-foreground"><Box className="size-3.5" />Web 客户端</span>
        <ChevronRight className="size-3 text-faint-foreground" />
        <span className="truncate font-medium">MUL-9417 收件箱未读计数在切换工作区后不刷新</span>
        <span className="ml-auto flex items-center gap-1 text-muted-foreground">
          <span className="flex size-7 items-center justify-center"><Bell className="size-4" /></span>
          <span className="flex size-7 items-center justify-center"><MoreHorizontal className="size-4" /></span>
          <span className="flex size-7 items-center justify-center rounded-md bg-muted text-foreground"><PanelRight className="size-4" /></span>
        </span>
      </header>
      <div className="flex min-h-0 flex-1">
        <main className="min-w-0 flex-1 overflow-y-auto">
          <div className="mx-auto max-w-[720px] px-8 py-8">
            <div className="text-caption text-muted-foreground">MUL-9417</div>
            <h1 className="mt-1 text-display-sm font-semibold">收件箱未读计数在切换工作区后不刷新</h1>
            <p className="mt-3 text-body-lg">在工作区 A 有 3 条未读，切到工作区 B 后，收件箱角标仍然显示 3，直到刷新页面才正确。</p>
            <div className="mt-7 border-b pb-2 text-label font-medium">活动</div>
            <Comment who="Jiayuan" when="11月17日"><span className="text-brand">@Lambda</span> 看一下，顺便把桌面端多窗口的同步也拆成子任务。</Comment>
            <Comment who="Lambda" when="8 分钟前">已修复：切换工作区时按工作区 ID 重置未读计数缓存，补了 e2e 用例。</Comment>
          </div>
        </main>
        <aside className="w-[316px] shrink-0 overflow-y-auto pt-3 pr-2 pb-4">
          <CardStack t={t} />
        </aside>
      </div>
    </div>
  );
}
