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
      en: "Make me a welcome page",
      zh: "帮我做一个欢迎页",
    },
    prompt: {
      en: "Make a small HTML page (with some CSS, maybe a touch of animation) that welcomes me to Multica. Paste the full HTML in a comment on this issue so I can copy it straight from there.",
      zh: "用 HTML + 简单 CSS 给我做一个欢迎页,庆祝我刚加入 Multica。可以加点小动效。做完后把完整 HTML 贴到这个 issue 的评论里,我直接复制就能用。",
    },
  },
};
