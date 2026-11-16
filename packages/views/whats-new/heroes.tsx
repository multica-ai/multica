"use client";

import { useState, type CSSProperties, type ReactNode } from "react";
import { inboxSnoozeOptions, type InboxSnoozePreset } from "@multica/core/inbox/snooze";
import {
  AlarmClock,
  AppWindow,
  Archive,
  ArrowUp,
  Bot,
  Check,
  ChevronDown,
  Clock,
  Ellipsis,
  Laptop,
  Paperclip,
  Plus,
  Quote,
  RotateCw,
  ScrollText,
  Search,
  Server,
  Square,
} from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
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

function MenuItem({ icon, active, children }: { icon: ReactNode; active?: boolean; children: ReactNode }) {
  return (
    <div
      className={cn(
        "flex h-7 items-center gap-2 rounded-md px-2 text-label whitespace-nowrap [&_svg]:size-3.5 [&_svg]:text-muted-foreground",
        active && "bg-accent",
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

export const THEMES_HERO: WhatsNewHero = { ground: "orange", Art: ThemesArt };
export const ENVIRONMENTS_HERO: WhatsNewHero = { ground: "ice", Art: EnvironmentsArt };
export const ANNOTATIONS_HERO: WhatsNewHero = { ground: "lime", Art: AnnotationsArt };
export const SNOOZE_HERO: WhatsNewHero = { ground: "brand", Art: SnoozeArt };
export const VOICE_HERO: WhatsNewHero = { ground: "orange", Art: VoiceArt };
