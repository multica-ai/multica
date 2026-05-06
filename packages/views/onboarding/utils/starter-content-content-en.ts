import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import type { ImportStarterIssuePayload } from "@multica/core/api";

// =============================================================================
// English starter-content body. Long-form markdown lives here (TypeScript,
// reviewed as UI). The orchestrator in starter-content-templates.ts picks
// between this file and starter-content-content-zh.ts based on the user's
// locale, then hands the result to buildImportPayload.
// =============================================================================

export const PROJECT = {
  title: "Getting Started",
  description:
    "A few things to try in Multica. Work through them at your own pace.",
};

interface WelcomeIssueText {
  title: string;
  description: string;
}

export function buildWelcomeIssueText(
  q: QuestionnaireAnswers,
  userName: string,
): WelcomeIssueText {
  const name = userName.trim() || "there";

  const header = [
    `Welcome to Multica! 👋`,
    ``,
    `This is your workspace's first issue. Below, your agent will reply in a moment — that's how work happens here: you write what you want, your agent (or a teammate) picks it up and replies in the comments.`,
    ``,
    `[Learn how Multica works →](https://multica.ai/docs/how-multica-works)`,
    ``,
    `---`,
    ``,
  ].join("\n");

  const sharedInstructions = [
    `In your first reply, please:`,
    ``,
    `1. **Introduce yourself briefly** — your name, your role, what you're good at.`,
    `2. **Explain how we work together in Multica**:`,
    `   - Assigning an issue to you **and** setting its status to **Todo** is what triggers you to start (Backlog pauses you)`,
    `   - @mentioning you inside a comment is for quick questions`,
    `   - **Workspace Context** (in Settings → General) is shared background every agent here sees`,
    `3. **Point them at the *Getting Started* project** in the sidebar and invite them to assign you a real task when they're ready.`,
    ``,
    `Keep it friendly and under 200 words. End with one short question that invites ${name} to reply.`,
  ].join("\n");

  const exploreInstructions = [
    `In your first reply, please:`,
    ``,
    `1. **Introduce yourself briefly** — your name, your role, what you're good at.`,
    `2. **Explain how we work together in Multica**:`,
    `   - Assigning an issue to you **and** setting its status to **Todo** triggers you to start (Backlog pauses you)`,
    `   - @mentioning you inside a comment is for quick questions`,
    `   - **Workspace Context** (in Settings → General) is shared background every agent here sees`,
    `3. **Point them at the *Getting Started* project** in the sidebar.`,
    ``,
    `Keep it friendly and under 200 words. End with a small, curious question — something like "what's something you've been wondering about lately?" — so ${name} has an easy way to reply without having to come up with a real task yet.`,
  ].join("\n");

  switch (q.use_case) {
    case "coding":
      return {
        title: "👋 Welcome to Multica — let's work together",
        description: `${header}Hi agent, this is ${name}'s first time using Multica. They plan to use you mostly for **coding work**.\n\n${sharedInstructions}`,
      };
    case "planning":
      return {
        title: "👋 Welcome to Multica — let's work together",
        description: `${header}Hi agent, this is ${name}'s first time using Multica. They want your help with **planning and breaking down work**.\n\n${sharedInstructions}`,
      };
    case "writing_research":
      return {
        title: "👋 Welcome to Multica — let's work together",
        description: `${header}Hi agent, this is ${name}'s first time using Multica. They'll use you for **research and writing** — drafting, summarizing, analysis.\n\n${sharedInstructions}`,
      };
    case "explore":
      return {
        title: "👋 Welcome to Multica — let's work together",
        description: `${header}Hi agent, this is ${name}'s first time using Multica. They're **exploring** what Multica can do — no specific goal yet.\n\n${exploreInstructions}`,
      };
    case "other": {
      const customUseCase = (q.use_case_other ?? "").trim();
      const contextLine = customUseCase
        ? `They told us they want to use you for: "${customUseCase}".`
        : `They haven't narrowed down their use case yet.`;
      return {
        title: "👋 Welcome to Multica — let's work together",
        description: `${header}Hi agent, this is ${name}'s first time using Multica. ${contextLine}\n\n${sharedInstructions}`,
      };
    }
    default:
      return {
        title: "👋 Welcome to Multica — let's work together",
        description: `${header}Hi agent, this is ${name}'s first time using Multica.\n\n${sharedInstructions}`,
      };
  }
}

export function buildAgentGuidedSubIssues(
  q: QuestionnaireAnswers,
): ImportStarterIssuePayload[] {
  const tier1: ImportStarterIssuePayload[] = [
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: "Learn how to trigger your agent on any issue",
      description: [
        `**Every issue has a right-side panel** called **Properties**. From there you control who works on what. Agents in Multica are triggered when an issue has:`,
        ``,
        `  Assignee = your agent  AND  Status = Todo (not Backlog)`,
        ``,
        `**Try it now**:`,
        `1. In the sidebar, click **New Issue** at the top (or press \`C\`)`,
        `2. Give it a title like "Test run: summarize our product in 3 bullets"`,
        `3. On the right panel, find **Assignee** → click → pick your agent`,
        `4. Find **Status** → click → pick **Todo**`,
        `5. Scroll down to Activity — a **Live card** appears as your agent starts working`,
        ``,
        `**⚠️ Gotcha**: new issues default to Backlog. Agents pause on backlog. A hint dialog will pop up the first time — it's telling you "flip to Todo to start".`,
        ``,
        `**You'll know it worked when**: the Live card shows your agent thinking, and the Status flips to **In Progress** automatically.`,
        ``,
        `[Learn about assigning issues →](https://multica.ai/docs/assigning-issues)`,
      ].join("\n"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: "Chat with your agent — no issue required",
      description: [
        `Not every question needs a whole issue. For quick back-and-forth, use the **Chat panel**.`,
        ``,
        `**Where to find it**: look at the **bottom-right corner of the screen** — there's a round button with a **💬 speech bubble icon**. If your agent is working, the button pulses. If there are unread replies, a red badge sits on top of it.`,
        ``,
        `**Try it now**:`,
        `1. Click the 💬 button → a panel slides in from the right`,
        `2. At the **bottom-left of the input box**, click the agent avatar → pick your agent from the dropdown`,
        `3. Type a quick question: "What can you help me with in this workspace?"`,
        `4. Press **Enter**`,
        ``,
        `**Bonus — @mention an agent inside a comment**: on any issue, scroll to the comment box at the bottom. Type \`@\` and a dropdown appears listing members, agents, and other issues. Pick an agent → write your question → send. The mentioned agent replies in the comments.`,
        ``,
        `**You'll know it worked when**: the agent replies in the chat panel (or comment thread) within a few seconds.`,
        ``,
        `[Learn about chat →](https://multica.ai/docs/chat)`,
      ].join("\n"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: "Write your Workspace Context",
      description: [
        `**Workspace Context** is a shared system prompt every agent in this workspace reads before starting any task. It's the single most impactful thing you can do to make agent replies sharper.`,
        ``,
        `**Where to find it**:`,
        `1. Open the **sidebar** → scroll to the bottom section labeled **Configure**`,
        `2. Click **Settings** (⚙️ gear icon, bottom-most item)`,
        `3. In the left-side tab list, under the **[Your Workspace Name]** group, click **General**`,
        `4. Scroll down to the **Context** textarea (placeholder says "Provide context to agents...")`,
        ``,
        `**Fill it with 3-5 lines**:`,
        `- Who you are (name, role)`,
        `- What you're building or working on`,
        `- How agents should behave (tone, style, defaults)`,
        ``,
        `**Example**:`,
        `> I'm a frontend engineer working on an AI-native task manager. Reply concisely in English. Always explain your reasoning. Prefer TypeScript over JavaScript.`,
        ``,
        `Click **Save**.`,
        ``,
        `**You'll know it worked when**: the next task you assign to an agent picks up details from this context without you explaining again.`,
        ``,
        `[Learn about workspaces →](https://multica.ai/docs/workspaces)`,
      ].join("\n"),
    },
  ];

  const tier2: ImportStarterIssuePayload[] = [];

  if (q.team_size === "team") {
    tier2.push({
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: "Invite your teammates",
      description: [
        `Multica works best when a small team shares agents.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → **Settings** (⚙️, bottom)`,
        `2. Left tab list → under **[Your Workspace]** group → click **Members** (people icon)`,
        `3. At the top of the page, click **Add member**`,
        `4. Enter their email, pick a role (**Owner / Admin / Member**)`,
        `5. Click **Send invite**`,
        ``,
        `They'll receive an email with a join link. Pending invites show in the collapsible "Pending Invitations" section below the member list — you can revoke from there.`,
        ``,
        `[Learn about members and roles →](https://multica.ai/docs/members-roles)`,
      ].join("\n"),
    });
  }

  if (q.role === "developer" || q.use_case === "coding") {
    tier2.push({
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: "Connect a Git repo",
      description: [
        `Once connected, any agent can clone, read, and propose changes to your repo when you assign it a task.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → **Settings** (⚙️)`,
        `2. Left tab list → under **[Your Workspace]** group → **Repositories** (folder with Git branch icon)`,
        `3. At the bottom of the list, click **+ Add repository**`,
        `4. Fill the two inline fields:`,
        `   - **URL** — e.g. \`https://github.com/you/repo.git\``,
        `   - **Description** — what this repo is for`,
        `5. Click **Save** at the top of the page`,
        ``,
        `Repeat for as many repos as you want to expose.`,
      ].join("\n"),
    });
  }

  tier2.push({
    status: "todo",
    priority: "medium",
    assign_to_self: true,
    title: "Create a second agent with a different role",
    description: [
      `Running a small team of focused agents beats a single generalist. One for coding, one for planning, one for writing — each with their own instructions.`,
      ``,
      `**Note**: nothing locks a "Coding Agent" to coding. Instructions are just a system prompt, editable anytime. The split is about keeping each one's replies sharp.`,
      ``,
      `**Where to find it**:`,
      `1. Sidebar → under **Workspace** group → click **Agents** (🤖 bot icon)`,
      `2. In the left list header, click the **+** button (top-right corner of the list)`,
      `3. Fill the 4 fields in order:`,
      `   - **Name** — e.g. "Planning Agent"`,
      `   - **Description** — "Breaks down loose ideas into scoped work"`,
      `   - **Visibility** — Workspace (shared) or Private (only you)`,
      `   - **Runtime** — pick from the dropdown (your connected runtime)`,
      `4. Click **Create**`,
      ``,
      `**You'll know it worked when**: the new agent appears in the Assignee dropdown on any issue, and shows up in the left list on the Agents page.`,
      ``,
      `[Learn about creating agents →](https://multica.ai/docs/agents-create)`,
    ].join("\n"),
  });

  const tier3: ImportStarterIssuePayload[] = [
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Polish your agent's Instructions",
      description: [
        `Creating an agent is just the start. The **Instructions tab** is where you shape how it behaves.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → **Agents** (🤖)`,
        `2. In the left list, click an agent you want to refine`,
        `3. In the right panel, you'll see tabs at the top including **Activity / Instructions / Skills / Environment / MCP / Custom Args**`,
        `4. Click **Instructions**`,
        `5. Edit the markdown — changes save automatically`,
        ``,
        `**Good instructions include**:`,
        `- The role/persona (e.g. "You're a senior TypeScript engineer")`,
        `- House rules (e.g. "Always propose tests alongside code")`,
        `- Output format (e.g. "Return a short summary first, details below")`,
        ``,
        `Workspace Context and agent Instructions stack — both are sent on every task. Keep Instructions specific to this agent; keep Context specific to the whole workspace.`,
        ``,
        `If this agent uses Claude Code MCP servers, configure that JSON in the **MCP** tab. Non-Claude runtimes currently ignore MCP config.`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Watch your agent live in action",
      description: [
        `**Heads-up task** — nothing to do now, just know this exists.`,
        ``,
        `When an agent is working on an issue, a **Live card** appears at the top of the **Activity** section (it sticks to the top of the viewport as you scroll).`,
        ``,
        `The card shows in real time:`,
        `- Which tool the agent is calling (e.g. reading a file, web search)`,
        `- Streaming thoughts and partial output`,
        `- Current status (thinking / tool-running / done / failed)`,
        ``,
        `After the run finishes, the **Task Run History** below the card lists every past run. Click **View transcript** on any row to open the full interactive transcript — a timeline of every message, thinking step, tool call, and result.`,
        ``,
        `**Try it next time you assign an agent**: keep the issue open and watch the Live card appear below the description.`,
        ``,
        `[Learn about tasks →](https://multica.ai/docs/tasks)`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Check your Inbox for @mentions and updates",
      description: [
        `When someone — or an agent — @mentions you or assigns you an issue, it lands in your **Inbox**.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → the top section (above the **Workspace** group) → click **Inbox** (📥 icon) — an unread count badge shows on the right if you have new items`,
        ``,
        `**How it works**:`,
        `- Left column: notification list, newest first`,
        `- Right column: the linked issue opens inline, and the specific comment that mentioned you is **auto-highlighted and scrolled into view**`,
        `- Top-right dropdown: **Mark all as read / Archive all / Archive all read / Archive completed** for bulk cleanup`,
        ``,
        `**Tip**: "Archive completed" is the fastest way to clear the noise from issues already finished.`,
        ``,
        `[Learn about the inbox →](https://multica.ai/docs/inbox)`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Set up an Autopilot for recurring work",
      description: [
        `**Autopilot** turns a prompt into a scheduled task. Every day/week/hour, it auto-creates an issue and assigns it to an agent.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → under **Workspace** group → click **Autopilot** (⚡ Zap icon)`,
        `2. If you have no autopilots yet, a grid of templates shows up — pick any one to pre-fill the dialog, or click **+ New autopilot** for a blank one`,
        `3. Fill: **Name** / **Prompt** / **Agent** / **Schedule** (frequency + time + timezone)`,
        `4. Click **Create**`,
        ``,
        `**Good first autopilots**: daily digest of GitHub activity, weekly "what's blocked" check, or a Monday-morning triage of any issues still in Backlog.`,
        ``,
        `[Learn about autopilots →](https://multica.ai/docs/autopilots)`,
      ].join("\n"),
    },
  ];

  return [...tier1, ...tier2, ...tier3];
}

export function buildSelfServeSubIssues(
  q: QuestionnaireAnswers,
): ImportStarterIssuePayload[] {
  const tier1: ImportStarterIssuePayload[] = [
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: "Install a runtime (Desktop app or CLI)",
      description: [
        `**Why this first**: no runtime = no agents can execute. Everything below Tier 1 waits on this.`,
        ``,
        `A **runtime** pairs the daemon (a small background process on your machine) with one AI coding tool — Claude Code, Codex, and so on. If you have several tools installed, you'll see one runtime per tool. The runtime is what executes the tasks your agents pick up.`,
        ``,
        `**Option A — Desktop app (macOS, recommended if you're on a Mac)**:`,
        `1. Go to [github.com/multica-ai/multica/releases/latest](https://github.com/multica-ai/multica/releases/latest) and download the \`.dmg\` for macOS`,
        `2. Install and open the app`,
        `3. Sign in with the same account — the daemon is built in, you're done`,
        ``,
        `**Option B — CLI (macOS, Linux, or Windows via WSL)**:`,
        `1. In a terminal, install the CLI:`,
        `   \`\`\``,
        `   curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash`,
        `   \`\`\``,
        `2. Then run setup (signs you in and starts a background daemon):`,
        `   \`\`\``,
        `   multica setup`,
        `   \`\`\``,
        `   The daemon keeps running after you close the terminal — you don't have to leave anything open.`,
        ``,
        `**Verify**: sidebar → bottom **Configure** section → **Runtimes** → you should see at least one connected runtime.`,
        ``,
        `[Learn about runtimes →](https://multica.ai/docs/daemon-runtimes)`,
      ].join("\n"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: "Create your first agent",
      description: [
        `**Prerequisite**: task above done (runtime connected).`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → under **Workspace** group → click **Agents** (🤖 bot icon)`,
        `2. In the left list header, click the **+** button (top-right corner of the list)`,
        `3. Fill the 4 fields in order:`,
        `   - **Name** — e.g. "My Coding Agent"`,
        `   - **Description** — one line about what it does`,
        `   - **Visibility** — Workspace (shared) or Private (only you)`,
        `   - **Runtime** — pick the one you just installed`,
        `4. Click **Create**`,
        ``,
        `**Note**: an agent is just an LLM + instructions + workspace access. Nothing locks a "Coding Agent" to coding — same agent can do research, writing, planning. Keep it flexible.`,
        ``,
        `**You'll know it worked when**: the new agent appears in the Assignee dropdown on any issue.`,
        ``,
        `[Learn about creating agents →](https://multica.ai/docs/agents-create)`,
      ].join("\n"),
    },
  ];

  const tier2: ImportStarterIssuePayload[] = [
    {
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: "Assign your first real task to your agent",
      description: [
        `**Prerequisite**: you have a runtime + agent from the two tasks above.`,
        ``,
        `**How Multica triggers agents**:`,
        `- Assign an issue to an agent`,
        `- Set status to **Todo** (not Backlog — backlog pauses agents)`,
        `- The agent picks it up automatically`,
        ``,
        `**Try it now**:`,
        `1. In the sidebar, click **New Issue** at the top (or press \`C\`)`,
        `2. Title: something you actually want done`,
        `3. On the right panel, find **Assignee** → click → pick your agent`,
        `4. Find **Status** → change from Backlog to **Todo**`,
        `5. Watch the agent reply in the comments and a **Live card** appear in Activity`,
        ``,
        `**⚠️ Gotcha**: new issues default to **Backlog**. You must flip to **Todo** to trigger the agent.`,
        ``,
        `[Learn about assigning issues →](https://multica.ai/docs/assigning-issues)`,
      ].join("\n"),
    },
    {
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: "Write your Workspace Context",
      description: [
        `**Workspace Context** is a shared system prompt every agent in this workspace reads before starting any task. It's the single most impactful thing you can do to make agent replies sharper.`,
        ``,
        `**Where to find it**:`,
        `1. Open the **sidebar** → scroll to the bottom section labeled **Configure**`,
        `2. Click **Settings** (⚙️ gear icon, bottom-most item)`,
        `3. In the left-side tab list, under the **[Your Workspace Name]** group, click **General**`,
        `4. Scroll down to the **Context** textarea`,
        ``,
        `**Fill it with 3-5 lines**:`,
        `- Who you are (name, role)`,
        `- What you're building or working on`,
        `- How agents should behave (tone, style, defaults)`,
        ``,
        `Click **Save**.`,
        ``,
        `**You'll know it worked when**: the next task you assign to an agent picks up details from this context without you explaining again.`,
        ``,
        `[Learn about workspaces →](https://multica.ai/docs/workspaces)`,
      ].join("\n"),
    },
  ];

  const tier3: ImportStarterIssuePayload[] = [
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Chat with an agent — once you've created one",
      description: [
        `**Prerequisite**: you've created at least one agent (Tier 1 #2).`,
        ``,
        `Not every question needs a whole issue. For quick back-and-forth, use the **Chat panel**.`,
        ``,
        `**Where to find it**: the **bottom-right corner of the screen** has a round button with a **💬 speech bubble icon**.`,
        ``,
        `**Try it**:`,
        `1. Click the 💬 button → a panel slides in from the right`,
        `2. At the bottom-left of the input box, pick an agent from the dropdown`,
        `3. Type a question → press **Enter**`,
        ``,
        `**Bonus**: inside any issue's comment box, type \`@\` to mention an agent or member.`,
        ``,
        `[Learn about chat →](https://multica.ai/docs/chat)`,
      ].join("\n"),
    },
  ];

  if (q.role === "developer" || q.use_case === "coding") {
    tier3.push({
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Connect a Git repo",
      description: [
        `Once connected, any agent can clone, read, and propose changes to your repo when you assign it a task.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → **Settings** (⚙️)`,
        `2. Left tab list → **Repositories** (folder with Git branch icon)`,
        `3. At the bottom of the list, click **+ Add repository**`,
        `4. Fill **URL** (e.g. \`https://github.com/you/repo.git\`) and **Description**`,
        `5. Click **Save** at the top of the page`,
      ].join("\n"),
    });
  }

  if (q.team_size === "team") {
    tier3.push({
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Invite your teammates",
      description: [
        `Multica works best when a small team shares agents.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → **Settings** (⚙️, bottom)`,
        `2. Left tab list → **Members** (people icon)`,
        `3. Click **Add member** → enter email → pick role → **Send invite**`,
        ``,
        `[Learn about members and roles →](https://multica.ai/docs/members-roles)`,
      ].join("\n"),
    });
  }

  tier3.push(
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Shape your agent's Instructions (once it's created)",
      description: [
        `**Prerequisite**: you have at least one agent.`,
        ``,
        `Creating an agent is just the start. The **Instructions tab** is where you shape how it behaves.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → **Agents** (🤖)`,
        `2. Click an agent in the left list`,
        `3. Right panel → click the **Instructions** tab (alongside Skills / Environment / MCP / Custom Args)`,
        `4. Edit the markdown — changes save automatically`,
        ``,
        `Workspace Context and agent Instructions stack — both are sent on every task. Keep Instructions specific to this agent; keep Context specific to the whole workspace.`,
        ``,
        `If this agent uses Claude Code MCP servers, configure that JSON in the **MCP** tab. Non-Claude runtimes currently ignore MCP config.`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Watch an agent work live (once you've assigned one a task)",
      description: [
        `**Heads-up task** — nothing to do now, just know this exists.`,
        ``,
        `When an agent is working on an issue, a **Live card** appears at the top of the **Activity** section (it sticks to the top of the viewport as you scroll).`,
        ``,
        `It shows in real time which tool the agent is calling, streaming thoughts, and current status. After the run finishes, the **Task Run History** below the card lists every past run — click **View transcript** to open the full timeline.`,
        ``,
        `[Learn about tasks →](https://multica.ai/docs/tasks)`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "Set up an Autopilot (once you have an agent)",
      description: [
        `**Prerequisite**: you have at least one agent.`,
        ``,
        `**Autopilot** turns a prompt into a scheduled task. Every day/week/hour, it auto-creates an issue and assigns it to an agent.`,
        ``,
        `**Where to find it**:`,
        `1. Sidebar → under **Workspace** group → click **Autopilot** (⚡ Zap icon)`,
        `2. Pick a template, or click **+ New autopilot** for a blank one`,
        `3. Fill: **Name** / **Prompt** / **Agent** / **Schedule** (frequency + time + timezone) → **Create**`,
        ``,
        `[Learn about autopilots →](https://multica.ai/docs/autopilots)`,
      ].join("\n"),
    },
  );

  return [...tier1, ...tier2, ...tier3];
}
