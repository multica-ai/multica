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
  title: { en: string; zh: string; ko: string; ja: string };
  prompt: { en: string; zh: string; ko: string; ja: string };
}

export const HELPER_STARTER_PROMPTS: Record<StarterCardId, StarterPrompt> = {
  intro: {
    title: {
      en: "Introduce Multica to me",
      zh: "简单介绍一下 Multica",
      ko: "Multica를 간단히 소개해 주세요",
      ja: "Multica を簡単に紹介してください",
    },
    prompt: {
      en: "Introduce Multica to me in 1–2 paragraphs. Cover what it is, the core concepts (workspace / issue / agent / runtime), and how it differs from tools like Linear or Jira.",
      zh: "用 1-2 段话简单介绍 Multica 给我。讲清楚它是什么、核心概念有哪些(workspace / issue / agent / runtime)、和 Linear / Jira 之类的工具核心区别在哪。",
      ko: "Multica를 1-2문단으로 간단히 소개해 주세요. 무엇인지, 핵심 개념(workspace / issue / agent / runtime)이 무엇인지, Linear나 Jira 같은 도구와 핵심적으로 어떻게 다른지 설명해 주세요.",
      ja: "Multica を1〜2段落で簡単に紹介してください。何であるか、中心となる概念(workspace / issue / agent / runtime)、そして Linear や Jira のようなツールと根本的にどう違うのかを説明してください。",
    },
  },
  tour: {
    title: {
      en: "Walk me through the core features",
      zh: "带我熟悉每个功能",
      ko: "핵심 기능을 안내해 주세요",
      ja: "主要な機能を案内してください",
    },
    prompt: {
      en: "Walk me through Multica's core features — issue, agent, squad, autopilot, chat. Pick one realistic scenario I might run into and explain how all these pieces fit together.",
      zh: "陪我熟悉 Multica 的每个核心功能 —— issue、agent、squad、autopilot、chat。挑一个我可能用得上的真实场景,讲讲这几个东西是怎么配合的。",
      ko: "Multica의 핵심 기능인 issue, agent, squad, autopilot, chat을 안내해 주세요. 제가 실제로 겪을 만한 상황 하나를 골라 이 요소들이 어떻게 함께 작동하는지 설명해 주세요.",
      ja: "Multica の主要な機能 — issue、agent、squad、autopilot、chat を案内してください。私が実際に遭遇しそうな現実的なシナリオを1つ選び、これらの要素がどう連携するのかを説明してください。",
    },
  },
  welcome_page: {
    title: {
      en: "Show me what Multica can do for me — as slides",
      zh: "用 slides 介绍 Multica 能为我做什么",
      ko: "Multica가 저에게 무엇을 해줄 수 있는지 슬라이드로 보여 주세요",
      ja: "Multica が私に何をしてくれるのかをスライドで見せてください",
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
      ko: `Multica가 저에게 무엇을 해줄 수 있는지 보여주는 단일 파일 HTML 슬라이드 덱을 만들어 주세요. 제 역할과 사용 사례에 맞춰 주세요(아래 "내 정보" 참고). 전체 HTML을 이 issue의 댓글에 fenced \`\`\`html 코드 블록으로 붙여 주세요. 그대로 복사해 \`multica-intro.html\`로 저장하고 브라우저에서 더블클릭해 열 수 있어야 합니다.

**출력 형식**
- 하나의 self-contained .html 파일. CSS / JS는 모두 inline. 의존성, 빌드 도구, 외부 이미지는 쓰지 마세요(시각 요소는 CSS로 생성한 gradient, geometric shape, inline SVG를 사용).
- 전체 5-8장:
  1. 제목 — "[내 역할]에게 Multica가 해줄 수 있는 일"
  2. 네 가지 핵심 개념 — workspace / issue / agent / runtime, 한 장
  3-6. 제 사용 사례에 맞춘 구체적인 시나리오 3-4개. 각 시나리오는 "X를 하고 싶을 때 → Multica는 이렇게 처리합니다" 형식
  7. 마무리 — 구체적인 다음 액션 하나

**Viewport 규칙(반드시 지킬 것)**
- 모든 \`.slide\`: \`height: 100vh; height: 100dvh; overflow: hidden;\`
- 모든 font-size와 spacing 값은 \`clamp(min, preferred, max)\` 사용. 고정 px / rem 금지.
- slide당 밀도: 제목 1개 + bullet 4개 이하, 또는 제목 1개 + 짧은 문단 2개. 넘치면 다음 slide로 분리.
- \`prefers-reduced-motion: reduce\`를 존중해 animation을 끄세요.

**미감(흔한 AI 결과물처럼 보이지 않게)**
- Fontshare나 Google Fonts에서 개성 있는 typeface를 고르세요. Inter, Roboto, Arial, system font는 쓰지 마세요.
- CSS variable로 일관된 palette를 정하세요: dominant color 하나 + sharp accent 하나. 흔한 "보라색 gradient + 흰 배경"은 피하세요.
- 배경은 layered gradient나 geometric pattern으로 분위기를 만들고, flat white는 쓰지 마세요.
- slide마다 한 번의 잘 짜인 load-in animation만 사용하세요(\`animation-delay\`로 stagger). CSS-only. 흩어진 micro-interaction은 금지.

**Navigation**
- ArrowLeft / ArrowRight와 Space로 이동. 모서리에 작은 page indicator를 두세요.

완료 후, 어떤 시나리오를 골랐고 왜 골랐는지 한 문장으로 요약해 주세요.`,
      ja: `Multica が私に何をしてくれるのかを示す、単一ファイルの HTML スライドデッキを作ってください。私の役割とユースケースに合わせてください(下の「私について」を参照)。完全な HTML を、この issue のコメントに fenced \`\`\`html コードブロックで貼り付けてください。そのままコピーして \`multica-intro.html\` として保存し、ブラウザでダブルクリックして開けるようにしてください。

**出力フォーマット**
- 1つの self-contained な .html。CSS / JS はすべて inline。依存関係なし、ビルドツールなし、外部画像なし(視覚要素は CSS で生成 — グラデーション、幾何学的な図形、inline SVG を使用)。
- 全体で5〜8枚のスライド:
  1. タイトル — 「[私の役割] に Multica ができること」
  2. 4つの中心概念 — workspace / issue / agent / runtime を1枚で
  3〜6. 私のユースケースに合わせた具体的なシナリオを3〜4個。それぞれ「X をしたいとき → Multica はこう処理します」の形で
  7. 締め — 具体的な次の一歩のアクション

**ビューポートのルール(必ず守ること)**
- すべての \`.slide\`: \`height: 100vh; height: 100dvh; overflow: hidden;\`
- すべての font-size と spacing は \`clamp(min, preferred, max)\` を使用。固定の px / rem は使わない。
- 1枚あたりの密度: 見出し1つ + bullet 4つ以下、または見出し1つ + 短い段落2つ。あふれたら次のスライドに分ける。
- \`prefers-reduced-motion: reduce\` を尊重する(アニメーションを無効化)。

**美しさ(ありがちな AI っぽい見た目を避ける)**
- Fontshare か Google Fonts から個性のある typeface を選ぶ。Inter、Roboto、Arial、システムフォントは使わない。
- CSS variable で一貫した palette を決める: 主となる色1つ + 鋭いアクセント1つ。ありがちな「紫のグラデーション + 白背景」は避ける。
- 背景: 雰囲気を出すために重ねたグラデーションや幾何学パターンを使い、のっぺりした白は使わない。
- アニメーション: スライドごとに、よく練られた load-in を1回だけ(\`animation-delay\` でずらす)。CSS のみ。散らばった micro-interaction は禁止。

**ナビゲーション**
- ArrowLeft / ArrowRight と Space で進む。隅に小さな page indicator を置く。

完成したら、私のためにどのシナリオを選び、なぜ選んだのかを一文で要約してください。`,
    },
  },
};
