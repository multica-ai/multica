import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import type { Lang } from "../voice-input/copy";
import { useStoryEnv } from "../voice-input/env";
import { TEXT, ThreadCard, type Preset } from "./parts";
import { DIRECTIONS, DIRECTION_BY_ID, type Direction, type DirectionId } from "./variants";

const meta = {
  title: "Comment box",
  id: "comment-box",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

const PAGE = {
  compareTitle: { zh: "评论框和回复框：五个方向", en: "Comment and reply box: five directions" },
  compareLead: {
    zh: "A 是线上现状，B 到 E 是重新设计。每个框都能直接输入，输入 @ 会出现智能体提示，点麦克风会进入录音条。⌘↵ 发送。",
    en: "A is what ships; B to E are redesigns. Every box is live: type to try it, type @ to bring up the agent chip, click the mic for the recording strip. ⌘↵ sends.",
  },
  try: { zh: "试一试", en: "Try it" },
  states: { zh: "所有状态", en: "Every state" },
  comment: { zh: "评论框", en: "Comment box" },
  reply: { zh: "回复框", en: "Reply box" },
  inThread: { zh: "讨论串里的回复框", en: "Reply box in a thread" },
  atFoot: { zh: "时间线底部的评论框", en: "Comment box at the foot of the timeline" },
};

const STATES: { id: string; label: Record<Lang, string>; preset: (lang: Lang) => Preset }[] = [
  { id: "empty", label: { zh: "空", en: "Empty" }, preset: () => ({}) },
  { id: "focused", label: { zh: "聚焦", en: "Focused" }, preset: () => ({ focused: true }) },
  {
    id: "typing",
    label: { zh: "输入中，会叫起智能体", en: "Typing, starts an agent" },
    preset: (lang) => ({ text: TEXT.sample[lang], focused: true }),
  },
  {
    id: "restart",
    label: { zh: "会重启运行", en: "Would restart a run" },
    preset: (lang) => ({ text: TEXT.sample[lang], focused: true, restart: true }),
  },
  { id: "recording", label: { zh: "录音中", en: "Recording" }, preset: () => ({ recording: true, focused: true }) },
];

const COMPARE_STATES = ["empty", "typing"] as const;

function Label({ children }: { children: ReactNode }) {
  return <div className="mb-2 text-caption text-muted-foreground">{children}</div>;
}

function ReplyFrame({ children }: { children: ReactNode }) {
  return <div className="rounded-lg bg-card px-4 py-2.5 ring-1 ring-border">{children}</div>;
}

function DirectionHeading({ direction, size = "row" }: { direction: Direction; size?: "row" | "page" }) {
  const { lang } = useStoryEnv();
  return (
    <>
      <div className="text-caption font-medium text-brand">{direction.letter}</div>
      {size === "page" ? (
        <h1 className="mt-1 text-title font-semibold">{direction.name[lang]}</h1>
      ) : (
        <h2 className="text-body font-semibold">{direction.name[lang]}</h2>
      )}
      <p className={size === "page" ? "mt-2 text-body text-muted-foreground" : "mt-0.5 text-caption text-muted-foreground"}>
        {direction.pitch[lang]}
      </p>
    </>
  );
}

function ComparePage() {
  const { lang } = useStoryEnv();
  const byId = Object.fromEntries(STATES.map((s) => [s.id, s]));
  return (
    <div className="min-h-screen bg-background p-10">
      <header className="max-w-3xl">
        <h1 className="text-title font-semibold">{PAGE.compareTitle[lang]}</h1>
        <p className="mt-1.5 text-body text-muted-foreground">{PAGE.compareLead[lang]}</p>
      </header>
      <div className="mt-8 divide-y divide-border">
        {DIRECTIONS.map((direction) => (
          <section key={`${direction.id}-${lang}`} className="flex gap-8 py-6">
            <div className="w-56 shrink-0">
              <DirectionHeading direction={direction} />
            </div>
            <div className="grid min-w-0 max-w-[1120px] flex-1 grid-cols-2 gap-x-6 gap-y-5">
              {COMPARE_STATES.map((state) => (
                <div key={`c-${state}`}>
                  <Label>
                    {PAGE.comment[lang]} · {byId[state]!.label[lang]}
                  </Label>
                  <direction.Comment preset={byId[state]!.preset(lang)} />
                </div>
              ))}
              {COMPARE_STATES.map((state) => (
                <div key={`r-${state}`}>
                  <Label>
                    {PAGE.reply[lang]} · {byId[state]!.label[lang]}
                  </Label>
                  <ReplyFrame>
                    <direction.Reply preset={byId[state]!.preset(lang)} />
                  </ReplyFrame>
                </div>
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}

function DirectionPage({ id }: { id: DirectionId }) {
  const { lang } = useStoryEnv();
  const direction = DIRECTION_BY_ID[id];
  return (
    <div key={lang} className="min-h-screen bg-background p-10">
      <div className="flex items-start gap-12">
        <div className="w-[800px] shrink-0 space-y-10">
          <section>
            <h2 className="mb-3 text-caption font-medium text-muted-foreground">{PAGE.try[lang]}</h2>
            <Label>{PAGE.inThread[lang]}</Label>
            <ThreadCard>
              <direction.Reply />
            </ThreadCard>
            <div className="mt-6">
              <Label>{PAGE.atFoot[lang]}</Label>
              <direction.Comment />
            </div>
          </section>
          <section>
            <h2 className="mb-3 text-caption font-medium text-muted-foreground">{PAGE.states[lang]}</h2>
            <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,1fr)] gap-x-6 gap-y-5">
              {STATES.map((state) => (
                <div key={state.id} className="contents">
                  <div>
                    <Label>
                      {PAGE.comment[lang]} · {state.label[lang]}
                    </Label>
                    <direction.Comment preset={state.preset(lang)} />
                  </div>
                  <div>
                    <Label>
                      {PAGE.reply[lang]} · {state.label[lang]}
                    </Label>
                    <ReplyFrame>
                      <direction.Reply preset={state.preset(lang)} />
                    </ReplyFrame>
                  </div>
                </div>
              ))}
            </div>
          </section>
        </div>
        <aside className="w-72 shrink-0 pt-1">
          <DirectionHeading direction={direction} size="page" />
          <ul className="mt-5 space-y-2 text-body">
            {direction.notes[lang].map((note) => (
              <li key={note} className="flex gap-2">
                <span className="mt-2 size-1 shrink-0 rounded-full bg-faint-foreground" />
                <span>{note}</span>
              </li>
            ))}
          </ul>
        </aside>
      </div>
    </div>
  );
}

export const Compare: Story = { name: "Compare all", render: () => <ComparePage /> };
export const Current: Story = { name: "A · Current", render: () => <DirectionPage id="current" /> };
export const Tidy: Story = { name: "B · Tidy", render: () => <DirectionPage id="tidy" /> };
export const GrowsOnFocus: Story = { name: "C · Grows on focus", render: () => <DirectionPage id="growing" /> };
export const SameAsChat: Story = { name: "D · Same as chat", render: () => <DirectionPage id="chat" /> };
export const LabelledSend: Story = { name: "E · Labelled send", render: () => <DirectionPage id="label" /> };
