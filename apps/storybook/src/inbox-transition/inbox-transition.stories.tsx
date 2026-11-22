import type { Meta, StoryObj } from "@storybook/react-vite";
import { Badge } from "@multica/ui/components/ui/badge";
import { useStoryEnv } from "../voice-input/env";
import { DIRECTIONS, type DirectionId } from "./directions";
import { InboxWorkbench } from "./inbox-workbench";

const meta = {
  title: "Inbox transition",
  id: "inbox-transition",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

function CompareAllPage() {
  const { lang } = useStoryEnv();
  return (
    <div className="min-h-screen bg-page-canvas">
      <div className="mx-auto max-w-[1380px] px-8 py-8">
        <header className="max-w-3xl">
          <h1 className="text-display-sm font-semibold">
            {lang === "zh" ? "Inbox 自动切下一条：五种过渡方案" : "Inbox auto-advance: five transition directions"}
          </h1>
          <p className="mt-2 text-body text-muted-foreground">
            {lang === "zh"
              ? "下面先给一个完整尺度的可交互工作台（顶部可直接切换 A–E、开关 0.4x 慢动作与第 4 条冷缓存），建议连续按 E 归档 4–5 条，再点「标记完成」或「发送 @Emacs 评论」对比自动归档手感。"
              : "Try the full-scale interactive workbench below (switch A–E, toggle 0.4x slow-mo, or test cold-cache #4+). Press E through several items, then click Mark Done or send an @Emacs comment to compare auto-archive pacing."}
          </p>
        </header>

        <div className="mt-6 overflow-hidden rounded-xl border border-surface-border shadow-sm">
          <InboxWorkbench initialDirection="buffer" allowSwitchDirection heightClass="h-[720px]" />
        </div>

        <section className="mt-8 grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-5">
          {DIRECTIONS.map((d) => (
            <article
              key={d.id}
              className="flex flex-col rounded-xl bg-surface p-4 ring-1 ring-surface-border"
            >
              <div className="flex items-center justify-between gap-2">
                <span className="text-caption font-semibold text-brand">{d.letter}</span>
                {d.recommended && (
                  <Badge variant="secondary" className="text-micro font-medium text-brand">
                    {lang === "zh" ? "推荐" : "Recommended"}
                  </Badge>
                )}
              </div>
              <h2 className="mt-1 text-body font-semibold text-foreground">{d.name[lang]}</h2>
              <p className="mt-1.5 text-caption text-muted-foreground">{d.pitch[lang]}</p>
              <ul className="mt-3 space-y-1.5 text-caption text-foreground/85">
                {d.mechanics[lang].map((line) => (
                  <li key={line} className="flex gap-1.5">
                    <span className="mt-1.5 size-1 shrink-0 rounded-full bg-faint-foreground" />
                    <span>{line}</span>
                  </li>
                ))}
              </ul>
              <dl className="mt-auto space-y-2 border-t border-border/60 pt-3 text-caption">
                <div>
                  <dt className="font-medium text-foreground">{lang === "zh" ? "适合" : "Best for"}</dt>
                  <dd className="text-muted-foreground">{d.bestFor[lang]}</dd>
                </div>
                <div>
                  <dt className="font-medium text-foreground">{lang === "zh" ? "代价" : "Trade-off"}</dt>
                  <dd className="text-muted-foreground">{d.tradeoff[lang]}</dd>
                </div>
              </dl>
            </article>
          ))}
        </section>
      </div>
    </div>
  );
}

function SingleDirectionStory({ id }: { id: DirectionId }) {
  return <InboxWorkbench initialDirection={id} allowSwitchDirection heightClass="h-screen" />;
}

export const CompareAll: Story = {
  name: "Compare all",
  render: () => <CompareAllPage />,
};

export const Current: Story = {
  name: "A · Current (baseline)",
  render: () => <SingleDirectionStory id="current" />,
};

export const CalmCrossfade: Story = {
  name: "B · Calm crossfade",
  render: () => <SingleDirectionStory id="crossfade" />,
};

export const DirectionalGlide: Story = {
  name: "C · Directional glide",
  render: () => <SingleDirectionStory id="glide" />,
};

export const AutoArchiveBuffer: Story = {
  name: "D · Auto-archive buffer (Rec)",
  render: () => <SingleDirectionStory id="buffer" />,
};

export const CardStack: Story = {
  name: "E · Card stack",
  render: () => <SingleDirectionStory id="card-stack" />,
};
