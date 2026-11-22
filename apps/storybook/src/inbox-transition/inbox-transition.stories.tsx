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
              ? "每条任务都包含 11–15 条长评论（3,000px+ 高度）和 3 张异步加载图片，顶部可切 0ms / 250ms / 600ms 请求延迟。建议：1）切到 A 在 250ms/600ms 下连按 E，观察 40px 跳动、骨架屏、顶部闪现瞬移和图片晚加载把评论顶出视口；2）切到 B/C/D/E 体验锚点预热（0ms 秒切）以及狂按 E 冲出预热窗口时的稳态降级；3）点「标记完成」或「快捷发送 @Emacs 评论」感受自动归档节奏。"
              : "Every issue has 11–15 long comments (3,000px+ tall) and 3 async-loading screenshots, with 0ms / 250ms / 600ms network presets. Try: (1) Direction A at 250ms/600ms to feel the 40px bar drop, skeleton flash, scroll teleport, and late-image shift; (2) B/C/D/E for warm 0ms switches and rapid E-E-E cold fallback; (3) Mark Done or @Emacs comment for auto-archive pacing."}
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
