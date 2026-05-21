import { HELPER_DESCRIPTION, HELPER_INSTRUCTIONS } from "./helper-instructions";

const HELPER_AGENT_NAME = "Multica Helper";

/**
 * Skip path, issue 2/2: "Create your first Multica Agent".
 *
 * Companion to install-runtime-issue.ts. The body is a FUNCTION (not a
 * static const) because it needs to embed:
 *   - A mention chip pointing at the install-runtime issue (so the user
 *     can jump to it from this issue) — requires the install-runtime
 *     issue's identifier + uuid, only known after that issue is created.
 *   - The full Helper markdown block in the user's language (so the
 *     embedded ```md fence matches the surrounding body language).
 *
 * Caller MUST create install-runtime first, then call this with its ids.
 */

/**
 * Step 2 of the skip-path bundle. Bilingual title.
 */
export const CREATE_AGENT_GUIDE_ISSUE_TITLE = {
  en: "Step 2 — Create your first Multica Agent",
  zh: "第 2 步 —— 创建你的第一个 Multica Agent",
} as const;

interface BodyOpts {
  lang: "en" | "zh";
  installRuntimeIdentifier: string;
  installRuntimeId: string;
}

export function getCreateAgentGuideBody(opts: BodyOpts): string {
  const mention = `[${opts.installRuntimeIdentifier}](mention://issue/${opts.installRuntimeId})`;
  if (opts.lang === "zh") {
    return zhBody(mention);
  }
  return enBody(mention);
}

function enBody(installRuntimeMention: string): string {
  return `Once your runtime is online (see ${installRuntimeMention}), build your first agent — Multica Helper. The prompt below is pre-written; just copy.

## 1. Open the new-agent screen

Go to **Agents** in the sidebar → click **New Agent**.

## 2. Pick the runtime you just installed

Select the runtime under "Runtime". If nothing shows up, the runtime isn't online yet — finish the install steps in ${installRuntimeMention}.

## 3. Copy each block into the matching field

**Name**
\`\`\`md
${HELPER_AGENT_NAME}
\`\`\`

**Description**
\`\`\`md
${HELPER_DESCRIPTION.en}
\`\`\`

**Instructions**
\`\`\`md
${HELPER_INSTRUCTIONS.en}
\`\`\`

## 4. Save → assign an issue

Hit **Create**. The new agent shows up in the workspace agent list.

Now create an issue (or reassign an existing one) → set assignee = Multica Helper → set status to **todo**. The runtime picks the task up within a few seconds and starts working. Watch progress in the issue's task panel.

## Where to go next

- **Skills** — reusable instruction packs you can attach to any agent.
- **Squads** — groups of agents that can be assigned together.
- **Autopilots** — scheduled or webhook-triggered runs.
- **Docs** — https://multica.ai/docs.`;
}

function zhBody(installRuntimeMention: string): string {
  return `等运行时上线（见 ${installRuntimeMention}）之后，把第一个 agent —— Multica Helper —— 建出来。下面的提示词已经写好，直接复制即可。

## 1. 打开新建 agent 页

在侧边栏点 **Agents** → 点 **New Agent**。

## 2. 选你刚装好的运行时

在 "Runtime" 下选它。如果什么都没有，说明运行时还没上线 —— 先按 ${installRuntimeMention} 把安装步骤走完。

## 3. 把下面三段分别复制到对应字段

**名称**
\`\`\`md
${HELPER_AGENT_NAME}
\`\`\`

**描述**
\`\`\`md
${HELPER_DESCRIPTION.zh}
\`\`\`

**指令**
\`\`\`md
${HELPER_INSTRUCTIONS.zh}
\`\`\`

## 4. 保存 → 分派 issue

点 **Create**。新 agent 会出现在 workspace 的 agent 列表里。

接着创建一个 issue（或把已有 issue 重新分派）→ 把 assignee 设成 Multica Helper → 状态切到 **todo**。运行时会在几秒内接走任务并开始工作。在 issue 的任务面板里看进度。

## 接下来去哪

- **Skills** —— 可复用的指令包，可挂到任何 agent 上。
- **Squads** —— 可一起被分派的一组 agent。
- **Autopilots** —— 定时或 webhook 触发的运行。
- **文档** —— https://multica.ai/docs。`;
}
