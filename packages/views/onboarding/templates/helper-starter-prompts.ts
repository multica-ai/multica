/**
 * The 3 starter tasks the Runtime-path Welcome Modal offers a user after
 * Multica Helper is created. Each card maps to ONE issue being created
 * via `api.createIssue` with:
 *   - title = STARTER_PROMPT.title[lang]
 *   - description = STARTER_PROMPT.prompt[lang]
 *   - assignee = the Helper agent
 *
 * Title + prompt are persisted to the DB so they live as TS constants
 * (NOT i18n JSON) — anything written to the DB must be available at
 * write time without depending on the i18n bundle. Card subtitles are
 * UI-only (shown inside the Modal preview) and stay in
 * `locales/.../onboarding.json` under `welcome_after_onboarding.runtime.cards.*.subtitle`.
 */

export const STARTER_CARD_IDS = ["intro", "tour", "welcome_page"] as const;
export type StarterCardId = (typeof STARTER_CARD_IDS)[number];

interface StarterPrompt {
  title: { en: string; zh: string };
  prompt: { en: string; zh: string };
}

export const HELPER_STARTER_PROMPTS: Record<StarterCardId, StarterPrompt> = {
  intro: {
    title: {
      en: "Introduce Multica to me",
      zh: "简单介绍一下 Multica",
    },
    prompt: {
      en: "Introduce Multica to me in 1–2 paragraphs. Cover what it is, the core concepts (workspace / issue / agent / runtime), and how it differs from tools like Linear or Jira.",
      zh: "用 1-2 段话简单介绍 Multica 给我。讲清楚它是什么、核心概念有哪些(workspace / issue / agent / runtime)、和 Linear / Jira 之类的工具核心区别在哪。",
    },
  },
  tour: {
    title: {
      en: "Walk me through the core features",
      zh: "带我熟悉每个功能",
    },
    prompt: {
      en: "Walk me through Multica's core features — issue, agent, squad, autopilot, chat. Pick one realistic scenario I might run into and explain how all these pieces fit together.",
      zh: "陪我熟悉 Multica 的每个核心功能 —— issue、agent、squad、autopilot、chat。挑一个我可能用得上的真实场景,讲讲这几个东西是怎么配合的。",
    },
  },
  welcome_page: {
    title: {
      en: "Show me what Multica can do for me — as slides",
      zh: "用 slides 介绍 Multica 能为我做什么",
    },
    prompt: {
      en: `Build me a single-file HTML slide deck that shows what Multica can do for me. Tailor it to my role and use case (see "About me" below). Paste the FULL HTML in a fenced \`\`\`html block in a comment on this issue so I can copy it straight out, save as \`multica-intro.html\`, and double-click to open it in a browser.

**Format**
- One self-contained .html, all CSS / JS inline. Zero dependencies, no build tools, no external images (use CSS-generated visuals — gradients, geometric shapes, SVG inline).
- 5–8 slides total:
  1. Title — "What Multica can do for [my role]"
  2. Four core concepts — workspace / issue / agent / runtime, one slide
  3–6. 3–4 concrete scenarios tailored to my use case, each in the form "When you want X → here's how Multica handles it"
  7. Closing — one specific next-step action

**Viewport rules (non-negotiable)**
- Every \`.slide\`: \`height: 100vh; height: 100dvh; overflow: hidden;\`
- All font-size and spacing values use \`clamp(min, preferred, max)\` — never fixed px / rem.
- Density per slide: 1 heading + ≤ 4 bullets, OR 1 heading + 2 short paragraphs. Overflow → split into another slide.
- Respect \`prefers-reduced-motion: reduce\` (disable animations).

**Aesthetic (avoid the AI-slop look)**
- Pick a distinctive typeface from Fontshare or Google Fonts. Do NOT use Inter, Roboto, Arial, or system fonts.
- Commit to a cohesive palette via CSS variables: one dominant color + one sharp accent. Avoid the cliché "purple gradient on white".
- Backgrounds: layered gradients or geometric patterns for atmosphere — never flat white.
- Animation: ONE well-orchestrated load-in per slide using staggered \`animation-delay\`. CSS-only. No scattered micro-interactions.

**Navigation**
- ArrowLeft / ArrowRight and Space to advance. Small page indicator in a corner.

When done, also reply with a one-sentence summary of which scenarios you picked for me and why.`,
      zh: `给我做一份单文件 HTML 演示稿,介绍 Multica 能为我做什么。根据我的角色和使用场景定制(见下面"关于我")。把完整 HTML 贴到这条 issue 的评论里的 \`\`\`html 代码块中,我直接复制下来存成 \`multica-intro.html\` 双击就能在浏览器里打开。

**产出格式**
- 一个自包含 .html,CSS / JS 全部 inline。零依赖、不用打包、不引外部图片(视觉用纯 CSS 生成 —— 渐变、几何形状、内联 SVG)。
- 5-8 张 slide:
  1. 标题页 —— "Multica 能为 [我的角色] 做什么"
  2. 四个核心概念 —— workspace / issue / agent / runtime,一张
  3-6. 3-4 个针对我使用场景的具体例子,形如"当你想做 X → Multica 是这样处理的"
  7. 收尾页 —— 一个具体的下一步动作

**视口约束(必须遵守)**
- 每个 \`.slide\`:\`height: 100vh; height: 100dvh; overflow: hidden;\`
- 所有 font-size 和 spacing 用 \`clamp(min, preferred, max)\`,不要写死 px / rem。
- 每张密度:1 个标题 + ≤ 4 个 bullet,或 1 个标题 + 2 段短段。超出就拆下一张。
- 兼容 \`prefers-reduced-motion: reduce\`(关动画)。

**审美(避免 AI 套路感)**
- 字体从 Fontshare 或 Google Fonts 选一个有辨识度的,不要用 Inter / Roboto / Arial / 系统字体。
- 用 CSS 变量统一调色板:一个主色 + 一个锐利的强调色。避免烂大街的"紫色渐变 + 白底"。
- 背景用层叠渐变或几何图案带氛围,不要纯白。
- 每张 slide 一次性的有节奏入场动画(用 \`animation-delay\` 错峰),CSS 实现。不要散落的微动效。

**导航**
- 左右方向键和空格切换,角落放一个小的页码指示。

做完后再用一句话告诉我你为我挑了哪几个场景以及为什么。`,
    },
  },
};
