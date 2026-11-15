import type { ReactNode } from "react";
import { Bell, Bot, CircleUser, Folder, FolderMinus, GripVertical, ListTodo, Plus } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useStoryEnv } from "../voice-input/env";
import type { Text, UpdateId } from "./copy";

/** Flat brand ground with a cropped product window, like a changelog hero. */
function HeroGround({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <div
      className={cn("relative h-40 overflow-hidden bg-brand", className)}
      style={{
        backgroundImage:
          "radial-gradient(color-mix(in oklab, var(--brand-foreground) 22%, transparent) 1px, transparent 1.4px)",
        backgroundSize: "14px 14px",
      }}
    >
      <div className="absolute top-6 right-[-28px] bottom-[-28px] left-7 rounded-lg bg-sidebar p-3 text-sidebar-foreground shadow-[var(--floating-shadow)] ring-1 ring-black/5">
        {children}
      </div>
    </div>
  );
}

function SectionLabel({ children, trailing }: { children: ReactNode; trailing?: ReactNode }) {
  return (
    <div className="flex h-5 items-center justify-between pr-9 text-micro font-medium text-muted-foreground">
      <span>{children}</span>
      {trailing}
    </div>
  );
}

function Row({ icon, label, trailing, className }: { icon: ReactNode; label: ReactNode; trailing?: ReactNode; className?: string }) {
  return (
    <div className={cn("flex h-6 items-center gap-2 rounded-md pr-9 pl-1.5 text-caption", className)}>
      <span className="flex size-4 shrink-0 items-center justify-center text-muted-foreground [&_svg]:size-3.5">{icon}</span>
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {trailing}
    </div>
  );
}

const t = (text: Text, lang: "zh" | "en") => text[lang];

function InboxHero() {
  const { lang } = useStoryEnv();
  const rows = [
    { who: "LP", title: "Stripe webhook retries double-charge", what: { zh: "Leo Park 提及了你", en: "Leo Park mentioned you" } },
    { who: "AK", title: "Sidebar inbox: archive on done", what: { zh: "Ana Kim 回复了你", en: "Ana Kim replied" } },
  ];
  return (
    <HeroGround>
      <div className="mb-1.5 text-label font-semibold">{t({ zh: "工作", en: "Work" }, lang)}</div>
      <div className="-mx-1.5 rounded-md px-1.5 pb-1 ring-2 ring-brand/45">
        <SectionLabel trailing={<span className="text-brand">3</span>}>{t({ zh: "收件箱", en: "Inbox" }, lang)}</SectionLabel>
        {rows.map((row) => (
          <div key={row.who} className="flex items-start gap-2 py-1 pr-9">
            <span className="mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-full bg-muted text-[8px] font-medium text-muted-foreground">
              {row.who}
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-caption font-medium">{row.title}</span>
              <span className="block truncate text-micro text-muted-foreground">{t(row.what, lang)}</span>
            </span>
            <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-brand" />
          </div>
        ))}
      </div>
    </HeroGround>
  );
}

function ViewsHero() {
  const { lang } = useStoryEnv();
  return (
    <HeroGround>
      <SectionLabel>{t({ zh: "视图", en: "Views" }, lang)}</SectionLabel>
      <Row icon={<ListTodo />} label={t({ zh: "全部任务", en: "All issues" }, lang)} />
      <Row
        icon={<CircleUser />}
        label={t({ zh: "我的工作", en: "My work" }, lang)}
        trailing={<span className="text-micro text-muted-foreground">8</span>}
        className="bg-sidebar-accent font-medium [&>span:first-child]:text-foreground"
      />
      <Row icon={<Bot />} label={t({ zh: "执行中", en: "Working" }, lang)} trailing={<span className="text-micro text-muted-foreground">2</span>} />
      <Row icon={<Bell />} label={t({ zh: "已订阅", en: "Subscribed" }, lang)} trailing={<span className="text-micro text-muted-foreground">14</span>} />
    </HeroGround>
  );
}

function NoProjectHero() {
  const { lang } = useStoryEnv();
  return (
    <HeroGround>
      <SectionLabel>{t({ zh: "项目", en: "Projects" }, lang)}</SectionLabel>
      <div className="relative -mx-1 rotate-[-1.5deg] rounded-md bg-background px-1 shadow-[var(--menu-shadow)] ring-1 ring-border">
        <Row
          icon={<FolderMinus />}
          label={t({ zh: "无项目", en: "No project" }, lang)}
          trailing={<Plus className="size-3.5 text-muted-foreground" />}
          className="font-medium"
        />
        <GripVertical className="absolute top-1/2 -left-4 size-3.5 -translate-y-1/2 text-brand-foreground" />
      </div>
      <Row icon={<Folder />} label="Desktop 1.4" trailing={<span className="text-micro text-muted-foreground">12</span>} />
      <Row icon={<Folder />} label="Billing" trailing={<span className="text-micro text-muted-foreground">5</span>} />
      <Row icon={<Folder />} label="Onboarding" trailing={<span className="text-micro text-muted-foreground">3</span>} />
    </HeroGround>
  );
}

function Toggle({ on }: { on: boolean }) {
  return (
    <span className={cn("flex h-3.5 w-6 shrink-0 items-center rounded-full p-0.5", on ? "justify-end bg-brand" : "bg-muted")}>
      <span className="size-2.5 rounded-full bg-background shadow-sm" />
    </span>
  );
}

function NotificationsHero() {
  const { lang } = useStoryEnv();
  const groups = [
    {
      label: { zh: "关于你", en: "About you" },
      rows: [
        { label: { zh: "提及我", en: "Mentions me" }, on: true },
        { label: { zh: "分配给我", en: "Assigned to me" }, on: true },
      ],
    },
    {
      label: { zh: "智能体", en: "Agents" },
      rows: [
        { label: { zh: "运行失败", en: "Run failed" }, on: true },
        { label: { zh: "运行完成", en: "Run finished" }, on: false },
      ],
    },
  ];
  return (
    <HeroGround>
      {groups.map((group) => (
        <div key={group.label.en} className="mb-1.5">
          <SectionLabel>{t(group.label, lang)}</SectionLabel>
          {group.rows.map((row) => (
            <div key={row.label.en} className="flex h-6 items-center justify-between pr-9 pl-1.5 text-caption">
              <span>{t(row.label, lang)}</span>
              <Toggle on={row.on} />
            </div>
          ))}
        </div>
      ))}
    </HeroGround>
  );
}

export const HEROES: Record<UpdateId, () => ReactNode> = {
  inbox: InboxHero,
  views: ViewsHero,
  "no-project": NoProjectHero,
  notifications: NotificationsHero,
};
