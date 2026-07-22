import type { ComponentType, ReactNode } from "react";
import {
  ArrowDown,
  ArrowRight,
  ArrowUp,
  Bot,
  FileText,
  MessagesSquare,
  Monitor,
  Play,
  Terminal,
} from "lucide-react";

type DiagramIcon = ComponentType<{ className?: string }>;

/**
 * Product-level view of one agent task. The diagram deliberately avoids
 * transport and queue implementation details: readers only need to see
 * what Multica records, what the connected computer executes, and where
 * progress returns.
 *
 * This component is Chinese-first while the onboarding docs are being
 * rewritten one locale at a time. Add localized copy before sharing it
 * with the other language pages.
 */
export function AgentWorkFlowDiagram() {
  return (
    <figure className="not-prose my-8 overflow-hidden rounded-xl border border-border bg-card">
      <figcaption className="sr-only">一次执行如何完成</figcaption>

      <DesktopDiagram />
      <MobileDiagram />

      <div className="border-t border-border bg-muted/20 px-5 py-3 text-center text-xs text-muted-foreground">
        工作从 issue 出发，进度和结果回到同一个 issue。
      </div>
    </figure>
  );
}

function DesktopDiagram() {
  return (
    <div className="hidden md:block">
      <section className="bg-muted/20 p-5">
        <LaneLabel>Multica 工作区</LaneLabel>
        <div className="grid grid-cols-[minmax(0,1fr)_72px_minmax(0,1fr)_72px_minmax(0,1fr)] items-stretch">
          <FlowNode
            icon={FileText}
            title="Issue"
            description="工作说明、讨论与负责人"
          >
            <span className="mt-3 inline-flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-[11px] font-medium text-muted-foreground">
              <Bot className="size-3" />
              负责人：智能体
            </span>
          </FlowNode>
          <HorizontalConnector label="创建任务" />
          <FlowNode
            icon={Play}
            title="执行任务（task）"
            description="智能体的一次具体执行，进入队列并留下记录"
            accent
          />
          <div aria-hidden />
          <FlowNode
            icon={MessagesSquare}
            title="时间线与执行日志"
            description="显示进度、回复和最终结果"
          />
        </div>
      </section>

      <div className="grid min-h-20 grid-cols-[minmax(0,1fr)_72px_minmax(0,1fr)_72px_minmax(0,1fr)] items-center px-5">
        <div className="col-span-2" aria-hidden />
        <VerticalConnector label="派发" direction="down" />
        <div aria-hidden />
        <VerticalConnector label="写回" direction="up" />
      </div>

      <section className="bg-[var(--primary)]/[0.035] p-5">
        <LaneLabel>你的电脑</LaneLabel>
        <div className="grid grid-cols-[minmax(0,1fr)_72px_minmax(0,1fr)_72px_minmax(0,1fr)] items-stretch">
          <div className="col-span-2" aria-hidden />
          <FlowNode
            icon={Monitor}
            title="运行时"
            description="领取任务，并调用对应的执行工具"
          />
          <HorizontalConnector label="调用" />
          <FlowNode
            icon={Terminal}
            title="AI 编程工具"
            description="读取代码、运行命令并生成结果"
          />
        </div>
      </section>
    </div>
  );
}

function MobileDiagram() {
  return (
    <div className="space-y-3 p-4 md:hidden">
      <LaneLabel>Multica 工作区</LaneLabel>
      <FlowNode
        icon={FileText}
        title="Issue"
        description="工作说明、讨论与负责人"
      >
        <span className="mt-3 inline-flex items-center gap-1.5 rounded-full bg-muted px-2.5 py-1 text-[11px] font-medium text-muted-foreground">
          <Bot className="size-3" />
          负责人：智能体
        </span>
      </FlowNode>
      <MobileConnector label="创建任务" />
      <FlowNode
        icon={Play}
        title="执行任务（task）"
        description="智能体的一次具体执行，进入队列并留下记录"
        accent
      />
      <MobileConnector label="派发到你的电脑" />

      <LaneLabel>你的电脑</LaneLabel>
      <FlowNode
        icon={Monitor}
        title="运行时"
        description="领取任务，并调用对应的执行工具"
      />
      <MobileConnector label="调用" />
      <FlowNode
        icon={Terminal}
        title="AI 编程工具"
        description="读取代码、运行命令并生成结果"
      />
      <MobileConnector label="进度和结果写回工作区" />

      <LaneLabel>回到 Multica</LaneLabel>
      <FlowNode
        icon={MessagesSquare}
        title="时间线与执行日志"
        description="显示进度、回复和最终结果"
      />
    </div>
  );
}

function FlowNode({
  icon: Icon,
  title,
  description,
  accent = false,
  children,
}: {
  icon: DiagramIcon;
  title: string;
  description: string;
  accent?: boolean;
  children?: ReactNode;
}) {
  return (
    <div
      className={
        accent
          ? "rounded-lg border border-[var(--primary)]/35 bg-background p-4 shadow-sm"
          : "rounded-lg border border-border bg-background p-4 shadow-sm"
      }
    >
      <div
        className={
          accent
            ? "mb-3 flex size-8 items-center justify-center rounded-md bg-[var(--primary)]/10 text-[var(--primary)]"
            : "mb-3 flex size-8 items-center justify-center rounded-md bg-muted text-foreground"
        }
      >
        <Icon className="size-4" />
      </div>
      <div className="text-sm font-semibold text-foreground">{title}</div>
      <div className="mt-1 text-xs leading-5 text-muted-foreground">
        {description}
      </div>
      {children}
    </div>
  );
}

function LaneLabel({ children }: { children: ReactNode }) {
  return (
    <div className="mb-3 text-[10px] font-semibold uppercase tracking-[0.12em] text-muted-foreground">
      {children}
    </div>
  );
}

function HorizontalConnector({ label }: { label: string }) {
  return (
    <div className="flex flex-col items-center justify-center gap-1 px-2 text-muted-foreground">
      <span className="text-[10px] font-medium">{label}</span>
      <ArrowRight className="size-4" aria-hidden />
    </div>
  );
}

function VerticalConnector({
  label,
  direction,
}: {
  label: string;
  direction: "up" | "down";
}) {
  return (
    <div className="flex flex-col items-center justify-center gap-1 text-muted-foreground">
      {direction === "up" ? <ArrowUp className="size-4" aria-hidden /> : null}
      <span className="text-[10px] font-medium">{label}</span>
      {direction === "down" ? (
        <ArrowDown className="size-4" aria-hidden />
      ) : null}
    </div>
  );
}

function MobileConnector({ label }: { label: string }) {
  return (
    <div className="flex items-center justify-center gap-2 py-1 text-muted-foreground">
      <ArrowDown className="size-4" aria-hidden />
      <span className="text-[10px] font-medium">{label}</span>
    </div>
  );
}
