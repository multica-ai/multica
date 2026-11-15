import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useStoryEnv } from "../voice-input/env";
import { UPDATES, type Text } from "./copy";
import { Carousel, Corner, Deck, Stack, type OptionProps } from "./options";
import { Frame, TryItStage } from "./stage";

const meta = {
  title: "Update cards",
  id: "update-cards",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

type OptionId = "deck" | "stack" | "carousel";

interface Option {
  letter: string;
  name: Text;
  pitch: Text;
  notes: Text[];
  play: (props: OptionProps) => ReactNode;
}

const OPTIONS: Record<OptionId, Option> = {
  deck: {
    letter: "A",
    name: { zh: "叠牌", en: "Deck" },
    pitch: {
      zh: "一张完整的卡片，其余更新叠在身后露出边。点「下一条」，这张飞走，下一张升上来。",
      en: "One full card with the rest stacked behind it. Next deals the card away and lifts the one behind.",
    },
    notes: [
      { zh: "一眼看出还有几条，圆点也能直接跳", en: "You can see how many are left, and the dots jump straight to one" },
      { zh: "点露出来的那条边也能翻到下一张", en: "Clicking the peeking edge also moves on" },
      { zh: "× 关掉全部，没看的留在帮助菜单的「新功能」里", en: "× dismisses them all; unread ones stay in What's new in the help menu" },
      { zh: "← → 翻页，Esc 关闭", en: "← → to page, Esc to close" },
    ],
    play: (p) => (
      <Corner>
        <Deck {...p} />
      </Corner>
    ),
  },
  stack: {
    letter: "B",
    name: { zh: "通知栈", en: "Stack" },
    pitch: {
      zh: "先只露出一行标题，悬停展开成列表，点开某一条才看完整卡片。最不打扰。",
      en: "Starts as one line. Hover to fan it into a list; open one to see the full card. The quietest.",
    },
    notes: [
      { zh: "不挡住页面，适合平时的小更新", en: "Covers little, so it suits everyday small updates" },
      { zh: "每条可以单独关掉，或者全部标为已读", en: "Dismiss one at a time, or mark them all read" },
      { zh: "图片要点开才看得到，大改动容易被略过", en: "The picture only shows once opened, so a big change is easy to skip" },
    ],
    play: (p) => (
      <Corner>
        <Stack {...p} />
      </Corner>
    ),
  },
  carousel: {
    letter: "C",
    name: { zh: "单卡翻页", en: "Carousel" },
    pitch: {
      zh: "一张卡片，用左右箭头翻页，图片横向滑过去。没有堆叠，最省地方。",
      en: "One card that pages with arrows; the picture slides sideways. No stack, the smallest footprint.",
    },
    notes: [
      { zh: "看不出后面还有没有，只靠「1 / 4」", en: "Only “1 / 4” says more are waiting" },
      { zh: "主按钮是去试试，不是下一条", en: "The main button goes to the feature, not to the next card" },
    ],
    play: (p) => (
      <Corner>
        <Carousel {...p} />
      </Corner>
    ),
  },
};

const IDS: OptionId[] = ["deck", "stack", "carousel"];

const PAGE = {
  title: { zh: "右下角的更新卡片：三个方案", en: "Update cards in the corner: three options" },
  lead: {
    zh: "三个方案都放在 GitHub 星标提示用的那个位置：页面卡片右下角，宽度也一样。左侧每个方案都有一个可以直接玩的故事，顶部可以切换卡片数量、重新开始。",
    en: "All three use the GitHub star prompt's spot: the page card's bottom-right corner, at the same width. Each option has a playable story in the sidebar, where the top bar switches the card count and restarts.",
  },
  states: { zh: "所有状态", en: "Every state" },
};

function Heading({ id }: { id: OptionId }) {
  const { lang } = useStoryEnv();
  const option = OPTIONS[id];
  return (
    <div className="w-64 shrink-0">
      <div className="text-caption font-medium text-brand">{option.letter}</div>
      <h2 className="text-body font-semibold">{option.name[lang]}</h2>
      <p className="mt-1 text-caption text-muted-foreground">{option.pitch[lang]}</p>
      <ul className="mt-3 space-y-1.5 text-caption">
        {option.notes.map((note) => (
          <li key={note.en} className="flex gap-2">
            <span className="mt-1.5 size-1 shrink-0 rounded-full bg-faint-foreground" />
            <span>{note[lang]}</span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function OverviewPage() {
  const { lang } = useStoryEnv();
  return (
    <div key={lang} className="min-h-screen bg-background p-10">
      <header className="max-w-3xl">
        <h1 className="text-title font-semibold">{PAGE.title[lang]}</h1>
        <p className="mt-1.5 text-body text-muted-foreground">{PAGE.lead[lang]}</p>
      </header>
      <div className="mt-8 grid grid-cols-[repeat(3,27rem)] gap-6">
        {IDS.map((id) => {
          const option = OPTIONS[id];
          return (
            <div key={id} className="flex flex-col gap-4">
              <Frame label={`${option.letter} · ${option.name[lang]}`}>{option.play({ updates: UPDATES })}</Frame>
              <Heading id={id} />
            </div>
          );
        })}
      </div>
    </div>
  );
}

function StatesPage() {
  const { lang } = useStoryEnv();
  const L = (zh: string, en: string) => (lang === "zh" ? zh : en);
  const rows: { id: OptionId; frames: { label: string; node: ReactNode }[] }[] = [
    {
      id: "deck",
      frames: [
        { label: L("第 1 张，身后还有 3 张", "Card 1, three behind"), node: <Corner><Deck updates={UPDATES} /></Corner> },
        { label: L("第 3 张，身后还有 1 张", "Card 3, one behind"), node: <Corner><Deck updates={UPDATES} initialIndex={2} /></Corner> },
        { label: L("最后一张：按钮变成「知道了」", "Last card: the button reads Got it"), node: <Corner><Deck updates={UPDATES} initialIndex={3} /></Corner> },
        { label: L("只有 1 条：没有堆叠和圆点", "A single update: no stack, no dots"), node: <Corner><Deck updates={UPDATES.slice(0, 1)} /></Corner> },
      ],
    },
    {
      id: "stack",
      frames: [
        { label: L("收起", "Collapsed"), node: <Corner><Stack updates={UPDATES} /></Corner> },
        { label: L("悬停展开", "Fanned out on hover"), node: <Corner><Stack updates={UPDATES} initialExpanded /></Corner> },
        { label: L("点开一条", "One opened"), node: <Corner><Stack updates={UPDATES} initialOpen="inbox" /></Corner> },
      ],
    },
    {
      id: "carousel",
      frames: [
        { label: L("第 1 页", "Page 1"), node: <Corner><Carousel updates={UPDATES} /></Corner> },
        { label: L("第 2 页", "Page 2"), node: <Corner><Carousel updates={UPDATES} initialIndex={1} /></Corner> },
      ],
    },
  ];
  return (
    <div key={lang} className="min-h-screen bg-background p-10">
      <h1 className="text-title font-semibold">{PAGE.states[lang]}</h1>
      <div className="mt-6 divide-y divide-border">
        {rows.map((row) => (
          <section key={row.id} className="flex gap-8 py-8">
            <Heading id={row.id} />
            <div className="flex flex-wrap gap-6">
              {row.frames.map((f) => (
                <Frame key={f.label} label={f.label}>
                  {f.node}
                </Frame>
              ))}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}

function TryIt({ id }: { id: OptionId }) {
  const { lang } = useStoryEnv();
  const option = OPTIONS[id];
  return (
    <TryItStage
      title={`${option.letter} · ${option.name[lang]}`}
      pitch={option.pitch[lang]}
      render={(p) => option.play({ ...p, keyboard: true })}
    />
  );
}

export const Overview: Story = { render: () => <OverviewPage /> };
export const ADeck: Story = { name: "A · Deck", render: () => <TryIt id="deck" /> };
export const BStack: Story = { name: "B · Stack", render: () => <TryIt id="stack" /> };
export const CCarousel: Story = { name: "C · Carousel", render: () => <TryIt id="carousel" /> };
export const AllStates: Story = { name: "All states", render: () => <StatesPage /> };
