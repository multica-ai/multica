"use client";

import { useState, type CSSProperties, type ReactNode } from "react";
import { QRCode } from "react-qr-code";
import { inboxSnoozeOptions, type InboxSnoozePreset } from "@multica/core/inbox/snooze";
import { createShortcutChord } from "@multica/core/shortcuts";
import {
  AlarmClock,
  AppWindow,
  Archive,
  ArrowUp,
  Bot,
  Check,
  ChevronDown,
  CircleCheck,
  CircleDot,
  Clock,
  Ellipsis,
  Flag,
  Folder,
  FolderKanban,
  FolderOpen,
  Laptop,
  Paperclip,
  Plus,
  Quote,
  RotateCw,
  ScrollText,
  Search,
  Server,
  Square,
  Trash2,
  Users,
  WeChat,
} from "@multica/ui/icons";
import { Kbd } from "@multica/ui/components/ui/kbd";
import { cn } from "@multica/ui/lib/utils";
import { ShortcutKeycaps } from "../common/shortcut-keycaps";
import { useLocale, useT } from "../i18n";
import { formatSnoozeTime } from "../inbox/components/inbox-snooze-time";

/**
 * Hero art for the What's new deck: a small mock of the feature built from
 * the product's own tokens and strings, so it follows the theme and locale.
 * The ground is a fixed color, picked per highlight for variety; each one
 * reads against both the light and the dark card.
 */
export type HeroGround = "brand" | "orange" | "lime" | "ice";

export interface WhatsNewHero {
  ground: HeroGround;
  Art: () => ReactNode;
}

const GROUNDS: Record<HeroGround, { color: string; dots: string }> = {
  brand: { color: "var(--brand)", dots: "rgb(255 255 255 / 0.22)" },
  orange: { color: "#ff6a2b", dots: "rgb(17 18 22 / 0.16)" },
  lime: { color: "#d9f45e", dots: "rgb(17 18 22 / 0.14)" },
  ice: { color: "#c9d7ff", dots: "rgb(17 18 22 / 0.12)" },
};

export function HeroFrame({ hero }: { hero: WhatsNewHero }) {
  const ground = GROUNDS[hero.ground];
  const style: CSSProperties = {
    backgroundColor: ground.color,
    backgroundImage: `radial-gradient(${ground.dots} 1px, transparent 1.4px)`,
    backgroundSize: "14px 14px",
  };
  const { Art } = hero;
  return (
    <div aria-hidden="true" inert className="relative h-44 overflow-hidden select-none" style={style}>
      <Art />
    </div>
  );
}

/** A cropped product surface; it bleeds off the right and bottom edges. */
function Surface({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div
      className={cn(
        "absolute overflow-hidden rounded-lg bg-surface-raised text-popover-foreground shadow-[var(--floating-shadow)] ring-1 ring-black/5 dark:ring-white/10",
        className,
      )}
    >
      {children}
    </div>
  );
}

function Menu({ className, children }: { className?: string; children: ReactNode }) {
  return (
    <div
      className={cn(
        "absolute z-10 rounded-lg bg-popover p-1 text-popover-foreground shadow-[var(--menu-shadow)] ring-1 ring-black/5 dark:ring-white/10",
        className,
      )}
    >
      {children}
    </div>
  );
}

function MenuItem({
  icon,
  active,
  danger,
  children,
}: {
  icon: ReactNode;
  active?: boolean;
  danger?: boolean;
  children: ReactNode;
}) {
  return (
    <div
      className={cn(
        "flex h-7 items-center gap-2 rounded-md px-2 text-label whitespace-nowrap [&_svg]:size-3.5 [&_svg]:text-muted-foreground",
        active && "bg-accent",
        danger && "text-destructive [&_svg]:text-destructive",
      )}
    >
      {icon}
      {children}
    </div>
  );
}

/** Names in the art: proper nouns that read the same in every locale. */
const SAMPLE = {
  author: "Multica",
  machine: "MacBook Pro",
  webApp: "Web app",
  desktopApp: "Desktop app",
  agent: "Builder",
};

/* ── Community themes: the theme store ─────────────────────────────── */

interface ThemeSample {
  name: string;
  version: string;
  paper: string;
  panel: string;
  ink: string;
  accent: string;
  inUse?: boolean;
}

const THEME_SAMPLES: ThemeSample[] = [
  { name: "Linen", version: "1.1.0", paper: "#f7f1e6", panel: "#ece3d2", ink: "#5a4632", accent: "#a0522d" },
  { name: "Flexoki", version: "1.0.0", paper: "#fffcf0", panel: "#f2f0e5", ink: "#100f0f", accent: "#205ea6", inUse: true },
  { name: "MS-DOS", version: "1.2.0", paper: "#0000aa", panel: "#000080", ink: "#ffffff", accent: "#55ffff" },
];

function ThemeThumb({ theme }: { theme: ThemeSample }) {
  return (
    <div className="flex h-12 gap-1.5 p-2" style={{ backgroundColor: theme.paper }}>
      <div className="flex w-1/4 flex-col gap-1 rounded-xs p-1" style={{ backgroundColor: theme.panel }}>
        {[70, 90, 55].map((w) => (
          <span key={w} className="h-1 rounded-full" style={{ width: `${w}%`, backgroundColor: theme.ink, opacity: 0.35 }} />
        ))}
      </div>
      <div className="flex flex-1 flex-col gap-1 pt-0.5">
        <span className="h-1.5 w-3/5 rounded-full" style={{ backgroundColor: theme.ink, opacity: 0.8 }} />
        {[92, 80, 86].map((w) => (
          <span key={w} className="h-1 rounded-full" style={{ width: `${w}%`, backgroundColor: theme.ink, opacity: 0.25 }} />
        ))}
        <span className="mt-auto h-2.5 w-8 rounded-xs" style={{ backgroundColor: theme.accent }} />
      </div>
    </div>
  );
}

function ThemesArt() {
  const { t } = useT("settings");
  return (
    <Surface className="top-4 left-6 w-[30rem]">
      <div className="flex items-center gap-2 px-4 pt-3">
        <span className="text-body font-semibold">{t(($) => $.appearance.store.title)}</span>
        <span className="rounded-sm bg-muted px-1.5 text-micro font-medium text-muted-foreground">
          {t(($) => $.appearance.import.beta)}
        </span>
        <div className="ml-3 flex h-7 w-44 items-center gap-1.5 rounded-md px-2 text-caption text-muted-foreground ring-1 ring-border">
          <Search className="size-3.5 shrink-0" />
          <span className="truncate">{t(($) => $.appearance.store.search)}</span>
        </div>
      </div>
      <div className="grid grid-cols-3 gap-2.5 p-4 pt-3">
        {THEME_SAMPLES.map((theme) => (
          <div key={theme.name} className="overflow-hidden rounded-lg ring-1 ring-border">
            <ThemeThumb theme={theme} />
            <div className="px-2.5 py-2">
              <div className="flex items-baseline gap-1.5">
                <span className="text-label font-medium">{theme.name}</span>
                <span className="text-micro text-muted-foreground tabular-nums">{theme.version}</span>
              </div>
              <div className="mt-1.5 flex items-center justify-between gap-2">
                <span className="truncate text-micro text-muted-foreground">
                  {t(($) => $.appearance.store.by, { author: SAMPLE.author })}
                </span>
                {theme.inUse ? (
                  <span className="inline-flex h-5 shrink-0 items-center gap-1 rounded-md bg-muted px-1.5 text-micro font-medium text-muted-foreground">
                    <Check className="size-3" />
                    {t(($) => $.appearance.store.in_use)}
                  </span>
                ) : (
                  <span className="inline-flex h-5 shrink-0 items-center rounded-md bg-primary px-1.5 text-micro font-medium text-primary-foreground">
                    {t(($) => $.appearance.store.install)}
                  </span>
                )}
              </div>
            </div>
          </div>
        ))}
      </div>
    </Surface>
  );
}

/* ── Environment controls: the issue sidebar section ───────────────── */

function EnvRow({ icon, name, meta, active }: { icon: ReactNode; name: string; meta: string; active?: boolean }) {
  const { t } = useT("issues");
  return (
    <div className={cn("relative flex items-start gap-2 rounded-md px-2 py-2", active && "bg-accent/60")}>
      <span className="mt-px text-success [&_svg]:size-3.5">{icon}</span>
      <div className="min-w-0 flex-1">
        <p className="flex items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-label font-medium">{name}</span>
          {active ? (
            <span className="grid size-5 place-items-center rounded-md bg-surface-raised text-muted-foreground shadow-sm ring-1 ring-border">
              <Ellipsis className="size-3.5" />
            </span>
          ) : (
            <span className="inline-flex shrink-0 items-center gap-1 text-caption text-success">
              <span className="size-1.5 rounded-full bg-success" />
              {t(($) => $.environments.state.running)}
            </span>
          )}
        </p>
        <p className="mt-1 truncate text-caption text-muted-foreground tabular-nums">{meta}</p>
      </div>
    </div>
  );
}

function EnvironmentsArt() {
  const { t } = useT("issues");
  const expires = t(($) => $.environments.expires_in_hours, { count: 23 });
  return (
    <>
      <Surface className="top-5 left-6 w-[19rem]">
        <div className="flex h-9 items-center gap-1 px-3">
          <span className="text-label font-medium">{t(($) => $.environments.section_title)}</span>
          <ChevronDown className="size-3 text-muted-foreground" />
          <span className="ml-auto text-caption text-muted-foreground">
            {t(($) => $.environments.running_count, { count: 2 })}
          </span>
          <Plus className="ml-1.5 size-3.5 text-muted-foreground" />
        </div>
        <div className="px-1 pb-2">
          <div className="flex min-h-7 items-center gap-1.5 px-2 text-micro text-muted-foreground">
            <Laptop className="size-3 shrink-0" />
            <span>{SAMPLE.machine}</span>
            <span className="rounded-xs bg-muted px-1 font-medium">{t(($) => $.environments.this_machine)}</span>
          </div>
          <EnvRow icon={<AppWindow />} name={SAMPLE.desktopApp} meta={expires} active />
          <EnvRow icon={<Server />} name={SAMPLE.webApp} meta={`localhost:3000 · ${expires}`} />
        </div>
      </Surface>
      <Menu className="top-[7.25rem] left-[8.75rem] w-44">
        <MenuItem icon={<AppWindow />} active>
          {t(($) => $.environments.focus)}
        </MenuItem>
        <MenuItem icon={<ScrollText />}>{t(($) => $.environments.view_logs)}</MenuItem>
        <MenuItem icon={<RotateCw />}>{t(($) => $.environments.restart)}</MenuItem>
        <MenuItem icon={<Square />}>{t(($) => $.environments.stop_confirm_item)}</MenuItem>
      </Menu>
    </>
  );
}

/* ── Chat annotations: a selected passage and its note ─────────────── */

function TextLine({ words, selected }: { words: number[]; selected?: [number, number] }) {
  return (
    <div className="flex h-5 items-center">
      {words.map((w, i) => {
        const inSelection = selected && i >= selected[0] && i <= selected[1];
        return (
          <span
            key={i}
            className={cn("flex h-5 items-center px-[3px]", inSelection && "bg-brand/20")}
          >
            <span
              className={cn("h-1.5 rounded-full", inSelection ? "bg-foreground/55" : "bg-muted-foreground/30")}
              style={{ width: w }}
            />
          </span>
        );
      })}
    </div>
  );
}

function AnnotationsArt() {
  const { t } = useT("issues");
  return (
    <>
      <Surface className="top-5 left-6 w-[22rem]">
        <div className="px-4 pt-3.5">
          <div className="flex items-center gap-2">
            <span className="grid size-5 place-items-center rounded-full bg-brand/10 text-brand">
              <Bot className="size-3" />
            </span>
            <span className="text-label font-medium">{SAMPLE.agent}</span>
          </div>
          <div className="mt-2 pl-7">
            <TextLine words={[34, 52, 22, 40, 60, 28]} selected={[1, 3]} />
            <TextLine words={[44, 30, 58, 36, 26, 48]} />
            <TextLine words={[50, 38, 24, 46]} />
          </div>
        </div>
        <div className="mx-3 mt-3 mb-3 flex items-center gap-2 rounded-lg px-2 py-1.5 ring-1 ring-border">
          <span className="inline-flex h-5 items-center gap-1 rounded-md bg-muted px-1.5 text-micro font-medium text-muted-foreground">
            <Quote className="size-3" />
            {t(($) => $.reply.annotations.count, { count: 1 })}
          </span>
        </div>
      </Surface>
      <Menu className="top-[5.4rem] left-[6rem] flex w-60 items-center gap-2 py-1 pr-1 pl-3">
        <span className="min-w-0 flex-1 truncate text-label text-muted-foreground">
          {t(($) => $.reply.annotations.note_placeholder)}
        </span>
        <span className="inline-flex h-6 shrink-0 items-center rounded-md bg-primary px-2 text-caption font-medium text-primary-foreground">
          {t(($) => $.reply.annotations.confirm_add_short)}
        </span>
      </Menu>
    </>
  );
}

/* ── Inbox snooze: a Work panel Inbox row and its snooze menu ──────── */

function InboxRowArt({ title, detail, agent, active }: { title: number; detail: number; agent?: boolean; active?: boolean }) {
  return (
    <div className={cn("flex items-start gap-2 rounded-md px-2 py-1.5", active && "bg-accent/60")}>
      <span
        className={cn(
          "mt-0.5 grid size-5 shrink-0 place-items-center rounded-full",
          agent ? "bg-brand/10 text-brand" : "bg-muted",
        )}
      >
        {agent && <Bot className="size-3" />}
      </span>
      <div className="flex min-w-0 flex-1 flex-col gap-1.5 pt-1">
        <span className="h-1.5 rounded-full bg-foreground/55" style={{ width: title }} />
        <span className="h-1.5 rounded-full bg-muted-foreground/30" style={{ width: detail }} />
      </div>
      {active ? (
        <span className="flex shrink-0 gap-0.5 text-muted-foreground">
          <span className="grid size-5 place-items-center rounded-md bg-surface-selected text-foreground">
            <AlarmClock className="size-3" />
          </span>
          <span className="grid size-5 place-items-center rounded-md">
            <Archive className="size-3" />
          </span>
        </span>
      ) : (
        <span className="mt-1.5 size-1.5 shrink-0 rounded-full bg-brand" />
      )}
    </div>
  );
}

const SNOOZE_PRESETS_SHOWN: InboxSnoozePreset[] = ["hours_1", "hours_3", "days_1", "days_3"];

function SnoozeArt() {
  const { t } = useT("inbox");
  const { t: tLayout } = useT("layout");
  const locale = useLocale();
  const [now] = useState(() => new Date());
  const presetLabel: Record<InboxSnoozePreset, string> = {
    hours_1: t(($) => $.snooze.hours_1),
    hours_3: t(($) => $.snooze.hours_3),
    days_1: t(($) => $.snooze.days_1),
    days_3: t(($) => $.snooze.days_3),
    weeks_1: t(($) => $.snooze.weeks_1),
  };
  const options = inboxSnoozeOptions(now).filter((option) => SNOOZE_PRESETS_SHOWN.includes(option.preset));
  return (
    <>
      <Surface className="top-4 left-6 w-[15rem]">
        <div className="flex h-8 items-center gap-1 px-3">
          <span className="text-label font-medium">{tLayout(($) => $.nav.inbox)}</span>
          <ChevronDown className="size-3 text-muted-foreground" />
          <span className="ml-auto text-caption text-muted-foreground tabular-nums">2</span>
        </div>
        <div className="px-1 pb-2">
          <InboxRowArt title={120} detail={88} agent active />
          <InboxRowArt title={96} detail={128} />
          <InboxRowArt title={136} detail={72} agent />
        </div>
      </Surface>
      <Menu className="top-[3.25rem] left-[10.75rem] w-52">
        <div className="px-2 py-1 text-caption font-medium text-muted-foreground">{t(($) => $.snooze.menu_title)}</div>
        {options.map((option, i) => (
          <MenuItem key={option.preset} icon={<Clock />} active={i === 0}>
            <span>{presetLabel[option.preset]}</span>
            <span className="ml-auto pl-2 text-caption text-muted-foreground tabular-nums">
              {formatSnoozeTime(option.until, now, locale)}
            </span>
          </MenuItem>
        ))}
      </Menu>
    </>
  );
}

/* ── Desktop voice input: hold the mic in a comment box and talk ───── */

/** A frozen take of the recorder's waveform; 0 is a silent dot. */
const VOICE_LEVELS = [0, 0, 0.3, 0.6, 0.9, 0.5, 0.75, 1, 0.65, 0.35, 0, 0.45, 0.8, 0.55, 0.95, 0.7, 0.4];

function VoiceArt() {
  const { t } = useT("editor");
  const { t: tIssues } = useT("issues");
  return (
    <div className="absolute top-[5.75rem] left-6 w-[20.5rem]">
      <div className="absolute bottom-full left-1/2 z-10 mb-3 flex h-10 -translate-x-1/2 items-center gap-2.5 whitespace-nowrap rounded-full bg-multica-ink pr-4 pl-3.5 text-multica-paper shadow-[var(--floating-shadow)]">
        <span className="size-2 shrink-0 rounded-full bg-[#ff5a52]" />
        <span className="flex h-5 items-center gap-[2px]">
          {VOICE_LEVELS.map((level, i) => (
            <span
              key={i}
              className={cn("w-[2.5px] rounded-full bg-current", level === 0 && "opacity-40")}
              style={{ height: level === 0 ? 2.5 : 2.5 + 17.5 * level }}
            />
          ))}
        </span>
        <span className="text-caption tabular-nums opacity-70">0:07</span>
        <span className="text-caption opacity-70">{t(($) => $.voice.release_to_insert)}</span>
      </div>
      <Surface className="relative">
        <div className="px-3 py-2 text-body text-muted-foreground">
          {tIssues(($) => $.comment.leave_comment_placeholder)}
        </div>
        <div className="flex h-9 items-center justify-end gap-1 px-2 pb-1 text-muted-foreground">
          <span className="grid size-7 place-items-center">
            <Paperclip className="size-4" />
          </span>
          <span className="grid size-7 place-items-center rounded-md bg-brand/10 text-brand">
            <span className="flex h-3.5 items-center gap-[2px]">
              {[0.7, 1, 0.8].map((h) => (
                <span key={h} className="w-[3px] rounded-full bg-current" style={{ height: `${h * 100}%` }} />
              ))}
            </span>
          </span>
          <span className="grid size-7 place-items-center rounded-full bg-muted text-faint-foreground">
            <ArrowUp className="size-4" />
          </span>
        </div>
      </Surface>
    </div>
  );
}

/* ── Chat wakeups: a reply that will report back, then the report ──── */

/** The wakeup in the art; a PR number reads the same in every locale. */
const WAKEUP_TITLE = "PR #128";

function ChatWakeupArt() {
  const { t } = useT("chat");
  const locale = useLocale();
  const ends = t(($) => $.wakeup.ends, {
    relative: new Intl.RelativeTimeFormat(locale, { numeric: "auto" }).format(24, "hour"),
  });
  return (
    <Surface className="top-4 left-6 w-[22rem]">
      <div className="px-4 pt-3.5">
        <div className="flex items-center gap-2">
          <span className="grid size-5 place-items-center rounded-full bg-brand/10 text-brand">
            <Bot className="size-3" />
          </span>
          <span className="text-label font-medium">{SAMPLE.agent}</span>
        </div>
        <div className="mt-2 pl-7">
          <TextLine words={[40, 56, 30, 48, 36]} />
          <TextLine words={[52, 34, 60, 28]} />
          <div className="mt-2 flex min-w-0 items-center gap-2 rounded-md border border-surface-border bg-surface px-2.5 py-1.5 text-caption">
            <AlarmClock className="size-3.5 shrink-0 text-brand" />
            <span className="min-w-0 flex-1 truncate">{t(($) => $.wakeup.card, { title: WAKEUP_TITLE })}</span>
            <span className="shrink-0 text-muted-foreground">{ends}</span>
          </div>
        </div>
        <div className="mt-3 flex items-center gap-2 pb-8 text-caption text-muted-foreground">
          <span className="h-px flex-1 bg-surface-border" />
          <AlarmClock className="size-3.5 shrink-0 text-brand" />
          <span className="min-w-0 truncate">{`${t(($) => $.wakeup.divider.condition)} · ${WAKEUP_TITLE}`}</span>
          <span className="h-px flex-1 bg-surface-border" />
        </div>
      </div>
    </Surface>
  );
}

/* ── Chat steering: typing into a reply that is still running ──────── */

function ChatSteeringArt() {
  const { t } = useT("chat");
  const { t: tIssues } = useT("issues");
  return (
    <>
      <Surface className="top-4 left-6 w-[21rem]">
        <div className="px-4 pt-3.5 pb-8">
          <div className="flex items-center gap-2">
            <span className="grid size-5 place-items-center rounded-full bg-brand/10 text-brand">
              <Bot className="size-3" />
            </span>
            <span className="text-label font-medium">{SAMPLE.agent}</span>
            <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
              <span className="size-1.5 rounded-full bg-brand" />
              {t(($) => $.status_pill.tools.making_edits)}
            </span>
          </div>
          <div className="mt-2 pl-7">
            <TextLine words={[44, 30, 58, 36, 48]} />
          </div>
        </div>
      </Surface>
      <Surface className="top-[5.5rem] left-10 w-[21rem] bg-surface ring-brand/40">
        <p className="px-3 pt-2 text-caption text-brand">{tIssues(($) => $.comment.recipient_steer)}</p>
        <div className="flex h-7 items-center gap-1.5 px-3">
          {[38, 54, 26, 44].map((w) => (
            <span key={w} className="h-1.5 rounded-full bg-foreground/55" style={{ width: w }} />
          ))}
          <span className="h-4 w-px bg-foreground" />
        </div>
        <div className="flex h-9 items-center justify-end gap-1 px-2 pb-1 text-muted-foreground">
          <span className="grid size-7 place-items-center">
            <Paperclip className="size-4" />
          </span>
          <span className="grid size-7 place-items-center rounded-full bg-primary text-primary-foreground">
            <ArrowUp className="size-4" />
          </span>
        </div>
      </Surface>
    </>
  );
}

/* ── WeChat: the connect QR, then a private chat with the agent ─────── */

/** WeChat's own chat colors, so the conversation reads as WeChat in every theme. */
const WECHAT_GREEN = "#07c160";
const WECHAT_BUBBLE = "#95ec69";

function WeixinArt() {
  const { t } = useT("settings");
  return (
    <>
      <Surface className="top-4 left-6 w-[14rem]">
        <div className="flex flex-col items-center px-4 pt-3 pb-6 text-center">
          <span className="text-label font-semibold">{t(($) => $.channels.connect_title)}</span>
          <div className="mt-2.5 grid size-20 place-items-center rounded-md border bg-white">
            <QRCode value="https://multica.ai" size={64} />
          </div>
          <span className="mt-2 text-caption text-muted-foreground">{t(($) => $.weixin.scan_hint)}</span>
        </div>
      </Surface>
      <Surface className="top-10 left-[16.25rem] w-[9.5rem]">
        <div className="flex h-8 items-center gap-1.5 px-2.5">
          <WeChat className="size-3.5" style={{ color: WECHAT_GREEN }} />
          <span className="text-label font-medium">{SAMPLE.agent}</span>
        </div>
        <div className="flex flex-col gap-1.5 bg-muted/60 p-2 pb-8">
          <span className="ml-auto flex h-6 items-center rounded-md px-2" style={{ backgroundColor: WECHAT_BUBBLE }}>
            <span className="h-1.5 w-12 rounded-full bg-black/40" />
          </span>
          <span className="flex w-fit flex-col gap-1 rounded-md bg-surface-raised px-2 py-1.5 ring-1 ring-black/5 dark:ring-white/10">
            <span className="h-1.5 w-20 rounded-full bg-foreground/45" />
            <span className="h-1.5 w-14 rounded-full bg-foreground/45" />
          </span>
          <span className="flex w-fit items-center gap-1.5 rounded-md bg-surface-raised px-2 py-1.5 text-muted-foreground ring-1 ring-black/5 dark:ring-white/10">
            <Paperclip className="size-3" />
            <span className="h-1.5 w-12 rounded-full bg-foreground/45" />
          </span>
        </div>
      </Surface>
    </>
  );
}

/* ── Project folders: dragging a project into a sidebar folder ──────── */

const FOLDER_SAMPLE = { open: "Atlas", target: "Orbit", dragged: "API" };

function FolderTreeRow({ icon, label, className }: { icon: ReactNode; label: string; className?: string }) {
  return (
    <div className={cn("flex h-7 items-center gap-2.5 rounded-md px-3 text-label [&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-muted-foreground", className)}>
      {icon}
      <span className="min-w-0 flex-1 truncate">{label}</span>
    </div>
  );
}

function ProjectFoldersArt() {
  const { t } = useT("layout");
  return (
    <>
      <Surface className="top-3 left-6 w-[15rem]">
        <div className="flex h-8 items-center gap-1 px-3">
          <span className="text-label font-medium">{t(($) => $.nav.projects)}</span>
          <ChevronDown className="size-3 text-muted-foreground" />
          <Plus className="ml-auto size-3.5 text-muted-foreground" />
        </div>
        <div className="space-y-px px-1 pb-8">
          <FolderTreeRow icon={<FolderOpen />} label={FOLDER_SAMPLE.open} />
          <div className="ml-5 space-y-px border-l border-surface-border pl-[5px]">
            <FolderTreeRow icon={<FolderKanban />} label={SAMPLE.webApp} />
            <FolderTreeRow icon={<FolderKanban />} label={SAMPLE.desktopApp} />
          </div>
          <FolderTreeRow icon={<Folder />} label={FOLDER_SAMPLE.target} className="bg-accent ring-1 ring-surface-border" />
        </div>
      </Surface>
      <Menu className="top-[7.6rem] left-[10.5rem] w-36 -rotate-3 py-0.5">
        <FolderTreeRow icon={<FolderKanban />} label={FOLDER_SAMPLE.dragged} />
      </Menu>
    </>
  );
}

/* ── Inbox page: the Inbox · Snoozed · Archived tabs over the list ──── */

function InboxTabsArt() {
  const { t } = useT("inbox");
  const tabs = [
    { id: "inbox", label: t(($) => $.page.title), count: 3 },
    { id: "snoozed", label: t(($) => $.list.snoozed_title), icon: <AlarmClock className="size-3" /> },
    { id: "archived", label: t(($) => $.list.archived_title), icon: <Archive className="size-3" /> },
  ];
  return (
    <Surface className="top-4 left-6 w-[19rem]">
      <div className="flex h-9 items-center gap-0.5 px-2">
        {tabs.map((tab, i) => (
          <span
            key={tab.id}
            className={cn(
              "flex h-6 items-center gap-1 rounded-md px-2 text-label whitespace-nowrap",
              i === 0 ? "bg-surface-selected font-medium text-foreground" : "text-muted-foreground",
            )}
          >
            {tab.icon}
            {tab.label}
            {tab.count !== undefined && <span className="text-brand tabular-nums">{tab.count}</span>}
          </span>
        ))}
      </div>
      <div className="px-1 pb-8">
        <InboxRowArt title={128} detail={92} agent active />
        <InboxRowArt title={104} detail={136} />
        <InboxRowArt title={140} detail={76} agent />
      </div>
    </Surface>
  );
}

/* ── Project overview: the tabs and the progress panel with its trend ── */

/** Done issues at the end of each recent week, rising toward the scope. */
const PROJECT_TREND = [1, 2, 2, 4, 5, 7, 8];
const PROJECT_SCOPE = 12;

function ProjectOverviewArt() {
  const { t } = useT("projects");
  const done = PROJECT_TREND[PROJECT_TREND.length - 1]!;
  const points = PROJECT_TREND.map((value, i) => `${(i / (PROJECT_TREND.length - 1)) * 100},${32 - (value / PROJECT_SCOPE) * 30}`).join(" ");
  return (
    <Surface className="top-4 left-6 w-[19.5rem]">
      <div className="flex h-9 items-center gap-4 border-b border-surface-border px-4 text-label whitespace-nowrap">
        <span className="flex h-full items-center border-b-2 border-foreground font-medium">{t(($) => $.detail.tab_overview)}</span>
        <span className="flex items-center gap-1 text-muted-foreground">
          {t(($) => $.detail.tab_issues)}
          <span className="tabular-nums">{PROJECT_SCOPE}</span>
        </span>
        <span className="text-muted-foreground">{t(($) => $.detail.tab_activity)}</span>
      </div>
      <div className="flex">
        <div className="min-w-0 flex-1 px-3 pt-3 pb-8">
          <TextLine words={[34, 46, 22]} />
          <TextLine words={[40, 28, 30]} />
          <TextLine words={[30, 44]} />
        </div>
        <div className="w-[9.5rem] shrink-0 border-l border-surface-border px-3 pt-2.5 pb-4">
          <div className="text-caption font-medium text-muted-foreground">{t(($) => $.detail.section_progress)}</div>
          <div className="mt-0.5 truncate text-label font-medium tabular-nums">
            {t(($) => $.detail.progress_panel.summary, { count: PROJECT_SCOPE, completed: done })}
          </div>
          <div className="mt-2 flex h-1.5 overflow-hidden rounded-full bg-muted">
            <span className="bg-info" style={{ width: `${(done / PROJECT_SCOPE) * 100}%` }} />
            <span className="bg-warning" style={{ width: `${(2 / PROJECT_SCOPE) * 100}%` }} />
          </div>
          <svg viewBox="0 0 100 32" preserveAspectRatio="none" className="mt-2.5 h-8 w-full overflow-visible text-info">
            <polyline points={points} fill="none" stroke="currentColor" strokeWidth="2" vectorEffect="non-scaling-stroke" />
          </svg>
        </div>
      </div>
    </Surface>
  );
}

/* ── Inbox triage: J/K/U keycaps and a Work panel row's right-click menu ── */

function InboxTriageArt() {
  const { t } = useT("inbox");
  const { t: tLayout } = useT("layout");
  return (
    <>
      <Surface className="top-4 left-6 w-[13.5rem]">
        <div className="flex h-8 items-center gap-1 px-3">
          <span className="text-label font-medium">{tLayout(($) => $.nav.inbox)}</span>
          <ChevronDown className="size-3 text-muted-foreground" />
          <span className="ml-auto text-caption text-muted-foreground tabular-nums">3</span>
        </div>
        <div className="px-1 pb-2">
          <InboxRowArt title={120} detail={88} agent />
          <InboxRowArt title={96} detail={128} active />
          <InboxRowArt title={136} detail={72} agent />
        </div>
      </Surface>
      <Menu className="top-[4.75rem] left-[9.75rem] w-44">
        <MenuItem icon={<CircleDot />} active>
          {t(($) => $.context_menu.mark_unread)}
        </MenuItem>
        <MenuItem icon={<Archive />}>{t(($) => $.context_menu.archive)}</MenuItem>
        <div className="mx-1 my-1 h-px bg-border" />
        <MenuItem icon={<Trash2 />} danger>
          {t(($) => $.context_menu.delete)}
        </MenuItem>
      </Menu>
      <div className="absolute top-5 right-5 flex gap-1.5">
        {["J", "K", "U"].map((key) => (
          <Kbd key={key} size="md" compact className="text-foreground">
            {key}
          </Kbd>
        ))}
      </div>
    </>
  );
}

/* ── Create with agent: frequent agent and squad chips, ⌥1–⌥3 ──────── */

const QUICK_SWITCH_ACTORS = [
  { name: SAMPLE.agent, squad: false },
  { name: "Scout", squad: false },
  { name: "Atlas", squad: true },
];

function ActorChipArt({ name, squad, selected }: { name: string; squad: boolean; selected?: boolean }) {
  return (
    <span
      className={cn(
        "inline-flex h-6 items-center gap-1.5 rounded-md pr-2 pl-1",
        selected ? "bg-surface-selected font-medium text-surface-selected-foreground" : "text-muted-foreground",
      )}
    >
      <span className="grid size-4 place-items-center rounded-full bg-brand/10 text-brand">
        {squad ? <Users className="size-2.5" /> : <Bot className="size-2.5" />}
      </span>
      {name}
    </span>
  );
}

function QuickSwitchArt() {
  const { t } = useT("modals");
  return (
    <>
      <Surface className="top-6 left-6 w-[24rem]">
        <div className="px-4 pt-3 text-caption text-muted-foreground">{t(($) => $.create_issue.agent_breadcrumb)}</div>
        <div className="px-3 pt-2 pb-3">
          <TextLine words={[44, 30, 58, 36, 48]} />
          <TextLine words={[52, 34]} />
        </div>
        <div className="flex items-center gap-2 px-4 pb-12 text-caption whitespace-nowrap">
          <span className="text-muted-foreground">{t(($) => $.create_issue.agent.created_by)}</span>
          <span className="flex items-center gap-0.5">
            {QUICK_SWITCH_ACTORS.map((actor, i) => (
              <ActorChipArt key={actor.name} name={actor.name} squad={actor.squad} selected={i === 1} />
            ))}
          </span>
          <span className="grid size-6 place-items-center text-muted-foreground">
            <ChevronDown className="size-3.5" />
          </span>
        </div>
      </Surface>
      <div className="dark absolute top-[5.1rem] left-[10.5rem] z-10 inline-flex items-center gap-1.5 rounded-tooltip bg-(--tooltip) py-1 pr-1.5 pl-2.5 text-caption whitespace-nowrap text-tooltip-foreground shadow-(--tooltip-shadow)">
        {QUICK_SWITCH_ACTORS[1]!.name}
        <ShortcutKeycaps shortcut={createShortcutChord("2", { alt: true })} decorative />
      </div>
    </>
  );
}

/* ── Issue relations: Blocked by in the sidebar, with what starts next ── */

const RELATION_BLOCKERS = [
  { identifier: "MUL-12", ended: true, title: [44, 58] },
  { identifier: "MUL-15", ended: false, title: [52, 36] },
];

function IssueRelationsArt() {
  const { t } = useT("issues");
  const ended = RELATION_BLOCKERS.filter((b) => b.ended).length;
  return (
    <Surface className="top-5 left-6 w-[19rem]">
      <div className="flex h-8 items-center px-3">
        <span className="text-label font-medium">{t(($) => $.relations.title)}</span>
        <Plus className="ml-auto size-3.5 text-muted-foreground" />
      </div>
      <div className="px-1.5 pb-12">
        <div className="flex items-center gap-1.5 px-1.5 pb-0.5 text-caption font-medium text-muted-foreground">
          <Flag className="size-3.5 text-destructive" />
          <span className="truncate">{t(($) => $.relations.blocked_by)}</span>
          <span className="ml-auto shrink-0 font-normal tabular-nums">
            {t(($) => $.waiting.marker_ended, { finished: ended, total: RELATION_BLOCKERS.length })}
          </span>
        </div>
        {RELATION_BLOCKERS.map((blocker) => (
          <div key={blocker.identifier} className="flex h-7 items-center gap-1.5 px-1.5 text-label">
            {blocker.ended ? (
              <CircleCheck className="size-3.5 shrink-0 text-success" />
            ) : (
              <CircleDot className="size-3.5 shrink-0 text-warning" />
            )}
            <span className="shrink-0 text-muted-foreground">{blocker.identifier}</span>
            <TextLine words={blocker.title} />
          </div>
        ))}
        <p className="px-1.5 pt-1 text-caption text-muted-foreground">
          {t(($) => $.waiting.marker_auto_start, { name: SAMPLE.agent })}
        </p>
      </div>
    </Surface>
  );
}

export const THEMES_HERO: WhatsNewHero = { ground: "orange", Art: ThemesArt };
export const ENVIRONMENTS_HERO: WhatsNewHero = { ground: "ice", Art: EnvironmentsArt };
export const ANNOTATIONS_HERO: WhatsNewHero = { ground: "lime", Art: AnnotationsArt };
export const SNOOZE_HERO: WhatsNewHero = { ground: "brand", Art: SnoozeArt };
export const VOICE_HERO: WhatsNewHero = { ground: "orange", Art: VoiceArt };
export const CHAT_WAKEUP_HERO: WhatsNewHero = { ground: "lime", Art: ChatWakeupArt };
export const CHAT_STEERING_HERO: WhatsNewHero = { ground: "orange", Art: ChatSteeringArt };
export const WEIXIN_HERO: WhatsNewHero = { ground: "ice", Art: WeixinArt };
export const PROJECT_FOLDERS_HERO: WhatsNewHero = { ground: "brand", Art: ProjectFoldersArt };
export const INBOX_TABS_HERO: WhatsNewHero = { ground: "lime", Art: InboxTabsArt };
export const PROJECT_OVERVIEW_HERO: WhatsNewHero = { ground: "ice", Art: ProjectOverviewArt };
export const INBOX_TRIAGE_HERO: WhatsNewHero = { ground: "orange", Art: InboxTriageArt };
export const QUICK_SWITCH_HERO: WhatsNewHero = { ground: "lime", Art: QuickSwitchArt };
export const ISSUE_RELATIONS_HERO: WhatsNewHero = { ground: "ice", Art: IssueRelationsArt };
