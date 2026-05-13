import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import type { ImportStarterIssuePayload } from "@multica/core/api";

// =============================================================================
// Chinese starter-content body. Mirrors starter-content-content-en.ts in
// shape; translated and adapted to the conventions in
// apps/docs/content/docs/developers/conventions.zh.mdx — task / issue /
// skill stay lowercase English; agent / runtime / daemon / workspace are
// translated; product UI labels (Properties, Assignee, Status, Activity,
// Live card, Inbox, Members, Settings, Runtimes, Configure, Workspace,
// Repositories, Instructions, Tasks, Skills, Autopilot, etc.) stay in
// English with English code-style framing matching the actual UI.
// =============================================================================

export const PROJECT = {
  title: "上手指南",
  description: "几件可以在 Multica 里上手试一试的事，按你的节奏走。",
};

interface WelcomeIssueText {
  title: string;
  description: string;
}

export function buildWelcomeIssueText(
  q: QuestionnaireAnswers,
  userName: string,
): WelcomeIssueText {
  const name = userName.trim() || "你";

  const header = [
    `欢迎来到 Multica！👋`,
    ``,
    `这是你工作区里的第一个 issue。下面你的智能体马上会回复——这就是 Multica 里工作的方式：你写下你想做的事，智能体（或同事）接手并在评论里回复。`,
    ``,
    `[了解 Multica 是怎么运转的 →](https://multica.ai/docs/zh/how-multica-works)`,
    ``,
    `---`,
    ``,
  ].join("\n");

  const sharedInstructions = [
    `请你在第一条回复里：`,
    ``,
    `1. **简短地自我介绍** —— 名字、定位、擅长的事。`,
    `2. **说明我们在 Multica 里怎么协作**：`,
    `   - 把 issue 分配给你 **并** 把状态置为 **Todo** 才会触发你开工（Backlog 状态会让你暂停）`,
    `   - 在评论里 @你 适合丢一个快速问题`,
    `   - **Workspace Context**（在 Settings → General）是这个工作区里每个智能体都会读到的共享背景`,
    `3. **把 ${name} 引到侧边栏的 *上手指南* 项目**，邀请 ${name} 准备好后给你分配一个真实的 task。`,
    ``,
    `语气友好、不超过 200 字。结尾抛一个简短的小问题让 ${name} 容易回复。`,
  ].join("\n");

  const exploreInstructions = [
    `请你在第一条回复里：`,
    ``,
    `1. **简短地自我介绍** —— 名字、定位、擅长的事。`,
    `2. **说明我们在 Multica 里怎么协作**：`,
    `   - 把 issue 分配给你 **并** 把状态置为 **Todo** 才会触发你开工（Backlog 状态会让你暂停）`,
    `   - 在评论里 @你 适合丢一个快速问题`,
    `   - **Workspace Context**（在 Settings → General）是这个工作区里每个智能体都会读到的共享背景`,
    `3. **把 ${name} 引到侧边栏的 *上手指南* 项目**。`,
    ``,
    `语气友好、不超过 200 字。结尾抛一个轻松的小问题——比如"最近你在琢磨什么有意思的事？"——让 ${name} 不必先想好一个真实任务也能轻松回复。`,
  ].join("\n");

  switch (q.use_case) {
    case "coding":
      return {
        title: "👋 欢迎来到 Multica —— 一起开工",
        description: `${header}你好智能体，这是 ${name} 第一次用 Multica。${name} 主要会让你做 **编码相关的工作**。\n\n${sharedInstructions}`,
      };
    case "planning":
      return {
        title: "👋 欢迎来到 Multica —— 一起开工",
        description: `${header}你好智能体，这是 ${name} 第一次用 Multica。${name} 希望你帮忙做 **规划与拆解工作**。\n\n${sharedInstructions}`,
      };
    case "writing_research":
      return {
        title: "👋 欢迎来到 Multica —— 一起开工",
        description: `${header}你好智能体，这是 ${name} 第一次用 Multica。${name} 会让你做 **调研和写作** —— 起草、摘要、分析。\n\n${sharedInstructions}`,
      };
    case "explore":
      return {
        title: "👋 欢迎来到 Multica —— 一起开工",
        description: `${header}你好智能体，这是 ${name} 第一次用 Multica。${name} 还在 **探索** Multica 能做什么 —— 暂时没有具体目标。\n\n${exploreInstructions}`,
      };
    case "other": {
      const customUseCase = (q.use_case_other ?? "").trim();
      const contextLine = customUseCase
        ? `${name} 告诉我们想让你做的事是："${customUseCase}"。`
        : `${name} 还没明确具体的使用场景。`;
      return {
        title: "👋 欢迎来到 Multica —— 一起开工",
        description: `${header}你好智能体，这是 ${name} 第一次用 Multica。${contextLine}\n\n${sharedInstructions}`,
      };
    }
    default:
      return {
        title: "👋 欢迎来到 Multica —— 一起开工",
        description: `${header}你好智能体，这是 ${name} 第一次用 Multica。\n\n${sharedInstructions}`,
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
      title: "学会怎么在任意 issue 上触发你的智能体",
      description: [
        `**每个 issue 右侧都有一个 Properties 面板**。从这里控制谁来做什么。Multica 里的智能体被触发的条件是 issue 同时满足：`,
        ``,
        `  Assignee = 你的智能体  AND  Status = Todo（不是 Backlog）`,
        ``,
        `**现在就试一下**：`,
        `1. 在侧边栏顶部点 **New Issue**（或按 \`C\`）`,
        `2. 标题写成类似"试运行：用 3 条 bullet 总结我们的产品"`,
        `3. 在右侧面板找到 **Assignee** → 点击 → 选你的智能体`,
        `4. 找到 **Status** → 点击 → 选 **Todo**`,
        `5. 滚动到 Activity —— 智能体一开工就会出现一张 **Live card**`,
        ``,
        `**⚠️ 容易踩**：新建的 issue 默认是 Backlog 状态，智能体在 Backlog 是暂停的。第一次会弹一个提示——意思就是"翻到 Todo 才会开工"。`,
        ``,
        `**怎么算成功**：Live card 里出现智能体在思考的状态，Status 自动翻到 **In Progress**。`,
        ``,
        `[关于把 issue 分配给智能体 →](https://multica.ai/docs/zh/assigning-issues)`,
      ].join("\n"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: "和智能体聊天 —— 不需要建 issue",
      description: [
        `不是每个问题都值得开一个 issue。要快速来回对话，用 **Chat 面板**。`,
        ``,
        `**在哪**：看屏幕 **右下角** —— 有一个圆形按钮，上面是一个 **💬 对话气泡**。智能体在工作时按钮会脉动；有未读回复时会有红色小角标。`,
        ``,
        `**现在就试一下**：`,
        `1. 点 💬 按钮 → 一个面板从右侧滑入`,
        `2. 在 **输入框左下角** 点智能体头像 → 从下拉里选你的智能体`,
        `3. 输入一个简短问题："这个工作区里你能帮我做什么？"`,
        `4. 按 **Enter**`,
        ``,
        `**附赠技巧 —— 在评论里 @智能体**：在任何 issue 底部的评论框里输入 \`@\`，会弹出一个下拉，列出成员、智能体和其他 issue。选一个智能体 → 写下问题 → 发送。被 @ 的智能体会在评论里回复。`,
        ``,
        `**怎么算成功**：智能体在几秒内通过 chat 面板（或评论里）回复。`,
        ``,
        `[关于聊天 →](https://multica.ai/docs/zh/chat)`,
      ].join("\n"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: "写一份 Workspace Context",
      description: [
        `**Workspace Context** 是一段共享系统提示，这个工作区里每个智能体在执行任何 task 之前都会读它。这是让智能体回复更精准、最有杠杆的一件事。`,
        ``,
        `**在哪**：`,
        `1. 打开 **侧边栏** → 滚到底部 **Configure** 区`,
        `2. 点 **Settings**（⚙️ 齿轮图标，最底部那个）`,
        `3. 左侧 tab 列表里，在 **[你的工作区名]** 分组下，点 **General**`,
        `4. 滚到 **Context** 文本框（占位符是"Provide context to agents..."）`,
        ``,
        `**写 3-5 行**：`,
        `- 你是谁（名字、定位）`,
        `- 你在做什么（产品、项目）`,
        `- 智能体应该怎么表现（语气、风格、默认行为）`,
        ``,
        `**例子**：`,
        `> 我是前端工程师，在做一个 AI-native 任务管理产品。回复用中文、简短。永远解释你的推理。优先选 TypeScript 而不是 JavaScript。`,
        ``,
        `点 **Save**。`,
        ``,
        `**怎么算成功**：你下次分给智能体一个 task，它会自动用上 context 里的信息，不需要你再解释一遍。`,
        ``,
        `[关于工作区 →](https://multica.ai/docs/zh/workspaces)`,
      ].join("\n"),
    },
  ];

  const tier2: ImportStarterIssuePayload[] = [];

  if (q.team_size === "team") {
    tier2.push({
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: "邀请同事加入",
      description: [
        `Multica 在小团队共享智能体的场景下最好用。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → **Settings**（⚙️，最底部）`,
        `2. 左侧 tab 列表 → 在 **[你的工作区]** 分组下 → 点 **Members**（人形图标）`,
        `3. 页面顶部点 **Add member**`,
        `4. 填邮箱、选角色（**Owner / Admin / Member**）`,
        `5. 点 **Send invite**`,
        ``,
        `他们会收到一封带加入链接的邮件。已发出的邀请会出现在成员列表下方"Pending Invitations"折叠区，从那里可以撤销。`,
        ``,
        `[关于成员与角色 →](https://multica.ai/docs/zh/members-roles)`,
      ].join("\n"),
    });
  }

  if (q.role === "developer" || q.use_case === "coding") {
    tier2.push({
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: "接入一个 Git 仓库",
      description: [
        `接入后，被分配 task 的智能体可以 clone、读取、提交对你仓库的修改。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → **Settings**（⚙️）`,
        `2. 左侧 tab 列表 → 在 **[你的工作区]** 分组下 → **Repositories**（带 Git 分支图标的文件夹）`,
        `3. 列表底部点 **+ Add repository**`,
        `4. 填两个字段：`,
        `   - **URL** —— 例如 \`https://github.com/you/repo.git\``,
        `   - **Description** —— 这个仓库是干嘛的`,
        `5. 在页面顶部点 **Save**`,
        ``,
        `想暴露多少个仓库就重复多少次。`,
      ].join("\n"),
    });
  }

  tier2.push({
    status: "todo",
    priority: "medium",
    assign_to_self: true,
    title: "再创建一个不同分工的智能体",
    description: [
      `跑一个分工明确的小型智能体团队，比一个万能选手更好用。一个写代码、一个做规划、一个写文 —— 各自有各自的指令。`,
      ``,
      `**说明**：没有什么强制把"编码智能体"锁死在编码上。指令本质就是 system prompt，随时可改。分开是为了让每个智能体的回复更聚焦。`,
      ``,
      `**在哪**：`,
      `1. 侧边栏 → 在 **Workspace** 分组下点 **Agents**（🤖 图标）`,
      `2. 在左侧列表头部点 **+** 按钮（列表右上角）`,
      `3. 按顺序填 4 个字段：`,
      `   - **Name** —— 例如"规划智能体"`,
      `   - **Description** —— "把零散想法拆成可执行的任务"`,
      `   - **Visibility** —— Workspace（共享）或 Private（仅自己）`,
      `   - **Runtime** —— 从下拉里选（你已连接的运行时）`,
      `4. 点 **Create**`,
      ``,
      `**怎么算成功**：新智能体出现在任意 issue 的 Assignee 下拉里，也出现在 Agents 页的左侧列表。`,
      ``,
      `[关于创建智能体 →](https://multica.ai/docs/zh/agents-create)`,
    ].join("\n"),
  });

  const tier3: ImportStarterIssuePayload[] = [
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "打磨智能体的 Instructions",
      description: [
        `创建智能体只是开始。**Instructions tab** 才是塑造它行为的地方。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → **Agents**（🤖）`,
        `2. 在左侧列表点你想调整的智能体`,
        `3. 在右侧面板顶部能看到一组 tab，包括 **Activity / Instructions / Skills / Environment / MCP / Custom Args**`,
        `4. 点 **Instructions**`,
        `5. 编辑 markdown —— 自动保存`,
        ``,
        `**好的指令包含**：`,
        `- 角色/人设（例如"你是一名资深 TypeScript 工程师"）`,
        `- 内部规则（例如"代码改动一定要附带测试"）`,
        `- 输出格式（例如"先一句话总结，再展开细节"）`,
        ``,
        `Workspace Context 和智能体 Instructions 是叠加的——每个 task 都会同时带上。Instructions 写这个智能体特有的，Context 写整个工作区都适用的。`,
        ``,
        `如果这个智能体要使用 Claude Code MCP server，请在 **MCP** tab 里配置 JSON。非 Claude 运行时目前会忽略 MCP 配置。`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "实时观看智能体工作",
      description: [
        `**了解性 task** —— 现在不用做什么，知道有这个东西就行。`,
        ``,
        `当智能体在某个 issue 上工作时，**Activity** 区顶部会出现一张 **Live card**（滚动时会粘在视口顶部）。`,
        ``,
        `Live card 实时展示：`,
        `- 智能体正在调用哪个工具（例如读文件、网页搜索）`,
        `- 流式输出的思考与中间结果`,
        `- 当前状态（thinking / tool-running / done / failed）`,
        ``,
        `执行结束后，Live card 下方的 **Task Run History** 列出每一次运行。任意一行点 **View transcript** 可以打开完整的可交互转录 —— 从消息、思考、工具调用到结果的完整时间线。`,
        ``,
        `**下次分配 task 时试一下**：保持 issue 打开，观察 Live card 在描述下方出现。`,
        ``,
        `[关于执行任务 →](https://multica.ai/docs/zh/tasks)`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "在 Inbox 里看 @提及与更新",
      description: [
        `当有人——或者智能体—— @你 或者把 issue 分给你时，事件会落到你的 **Inbox**。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → 顶部分区（**Workspace** 分组上方）→ 点 **Inbox**（📥 图标）—— 有新消息时右侧会显示未读角标`,
        ``,
        `**怎么用**：`,
        `- 左栏：通知列表，最新在上`,
        `- 右栏：关联的 issue 内嵌打开，**自动高亮并滚动到** @你的那条具体评论`,
        `- 右上下拉：**Mark all as read / Archive all / Archive all read / Archive completed** 用于批量整理`,
        ``,
        `**小技巧**："Archive completed" 是清掉已经完成 issue 噪音最快的方式。`,
        ``,
        `[关于收件箱 →](https://multica.ai/docs/zh/inbox)`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "用 Autopilot 处理周期性工作",
      description: [
        `**Autopilot** 把一段 prompt 变成定时 task。每天/每周/每小时自动建一个 issue 并分给智能体。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → 在 **Workspace** 分组下点 **Autopilot**（⚡ 闪电图标）`,
        `2. 还没有 autopilot 时，会出现一组模板——任选一个会预填弹窗，或者点 **+ New autopilot** 从空白开始`,
        `3. 填：**Name** / **Prompt** / **Agent** / **Schedule**（频率 + 时间 + 时区）`,
        `4. 点 **Create**`,
        ``,
        `**第一个 autopilot 可以试什么**：每日 GitHub 活动摘要、每周"哪些 issue 被卡住"巡检、每周一早上整理还停在 Backlog 的 issue。`,
        ``,
        `[关于自动化 →](https://multica.ai/docs/zh/autopilots)`,
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
      title: "装一个运行时（桌面应用 或 CLI）",
      description: [
        `**为什么先做这个**：没有运行时 = 智能体跑不了任何 task。Tier 1 之下的所有事情都等这个。`,
        ``,
        `**运行时**是守护进程（一个跑在你机器上的小后台进程）和一款 AI 编程工具——Claude Code、Codex 等等——的组合。装了多款工具就会出现多个运行时。运行时是真正执行智能体接到的 task 的那一层。`,
        ``,
        `**方案 A —— 桌面应用（macOS，Mac 推荐）**：`,
        `1. 去 [github.com/multica-ai/multica/releases/latest](https://github.com/multica-ai/multica/releases/latest) 下载 macOS 的 \`.dmg\``,
        `2. 安装并打开`,
        `3. 用同一个账号登录 —— 守护进程内置，到此结束`,
        ``,
        `**方案 B —— CLI（macOS、Linux 或 Windows + WSL）**：`,
        `1. 在终端装 CLI：`,
        `   \`\`\``,
        `   curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash`,
        `   \`\`\``,
        `2. 跑 setup（登录并启动后台守护进程）：`,
        `   \`\`\``,
        `   multica setup`,
        `   \`\`\``,
        `   守护进程会在终端关闭后继续运行 —— 不需要保留终端窗口。`,
        ``,
        `**验证**：侧边栏 → 底部 **Configure** 区 → **Runtimes** → 应该至少看到一个已连接的运行时。`,
        ``,
        `[关于守护进程与运行时 →](https://multica.ai/docs/zh/daemon-runtimes)`,
      ].join("\n"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: "创建你的第一个智能体",
      description: [
        `**前置条件**：上面那条 task 完成（运行时已连接）。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → 在 **Workspace** 分组下点 **Agents**（🤖 图标）`,
        `2. 在左侧列表头部点 **+** 按钮（列表右上角）`,
        `3. 按顺序填 4 个字段：`,
        `   - **Name** —— 例如"我的编码智能体"`,
        `   - **Description** —— 一句话说它做什么`,
        `   - **Visibility** —— Workspace（共享）或 Private（仅自己）`,
        `   - **Runtime** —— 选你刚才装的那个`,
        `4. 点 **Create**`,
        ``,
        `**说明**：智能体本质上就是 LLM + 指令 + 工作区访问权限。没有什么强制把"编码智能体"锁死在编码上 —— 同一个智能体可以做调研、写作、规划。保持灵活。`,
        ``,
        `**怎么算成功**：新智能体出现在任意 issue 的 Assignee 下拉里。`,
        ``,
        `[关于创建智能体 →](https://multica.ai/docs/zh/agents-create)`,
      ].join("\n"),
    },
  ];

  const tier2: ImportStarterIssuePayload[] = [
    {
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: "把第一个真实 task 分给智能体",
      description: [
        `**前置条件**：上面两条 task 都做完，你已经有运行时 + 智能体。`,
        ``,
        `**Multica 怎么触发智能体**：`,
        `- 把 issue 分给智能体`,
        `- 状态置为 **Todo**（不是 Backlog —— Backlog 会让智能体暂停）`,
        `- 智能体自动接手`,
        ``,
        `**现在就试一下**：`,
        `1. 在侧边栏顶部点 **New Issue**（或按 \`C\`）`,
        `2. 标题：你真正想做的事`,
        `3. 在右侧面板找到 **Assignee** → 点击 → 选你的智能体`,
        `4. 找到 **Status** → 从 Backlog 改为 **Todo**`,
        `5. 看智能体在评论里回复，Activity 里出现 **Live card**`,
        ``,
        `**⚠️ 容易踩**：新 issue 默认是 **Backlog**。必须翻到 **Todo** 才会触发智能体。`,
        ``,
        `[关于把 issue 分配给智能体 →](https://multica.ai/docs/zh/assigning-issues)`,
      ].join("\n"),
    },
    {
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: "写一份 Workspace Context",
      description: [
        `**Workspace Context** 是一段共享系统提示，这个工作区里每个智能体在执行任何 task 之前都会读它。这是让智能体回复更精准、最有杠杆的一件事。`,
        ``,
        `**在哪**：`,
        `1. 打开 **侧边栏** → 滚到底部 **Configure** 区`,
        `2. 点 **Settings**（⚙️ 齿轮图标，最底部那个）`,
        `3. 左侧 tab 列表里，在 **[你的工作区名]** 分组下，点 **General**`,
        `4. 滚到 **Context** 文本框`,
        ``,
        `**写 3-5 行**：`,
        `- 你是谁（名字、定位）`,
        `- 你在做什么（产品、项目）`,
        `- 智能体应该怎么表现（语气、风格、默认行为）`,
        ``,
        `点 **Save**。`,
        ``,
        `**怎么算成功**：你下次分给智能体一个 task，它会自动用上 context 里的信息，不需要你再解释一遍。`,
        ``,
        `[关于工作区 →](https://multica.ai/docs/zh/workspaces)`,
      ].join("\n"),
    },
  ];

  const tier3: ImportStarterIssuePayload[] = [
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "和智能体聊天 —— 创建之后再做",
      description: [
        `**前置条件**：你至少创建了一个智能体（Tier 1 #2）。`,
        ``,
        `不是每个问题都值得开一个 issue。要快速来回对话，用 **Chat 面板**。`,
        ``,
        `**在哪**：屏幕 **右下角** 有一个圆形按钮，上面是 **💬 对话气泡**。`,
        ``,
        `**试一下**：`,
        `1. 点 💬 按钮 → 一个面板从右侧滑入`,
        `2. 在输入框左下角，从下拉里选一个智能体`,
        `3. 输入问题 → 按 **Enter**`,
        ``,
        `**附赠技巧**：在任意 issue 的评论框里输入 \`@\` 可以提及智能体或成员。`,
        ``,
        `[关于聊天 →](https://multica.ai/docs/zh/chat)`,
      ].join("\n"),
    },
  ];

  if (q.role === "developer" || q.use_case === "coding") {
    tier3.push({
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "接入一个 Git 仓库",
      description: [
        `接入后，被分配 task 的智能体可以 clone、读取、提交对你仓库的修改。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → **Settings**（⚙️）`,
        `2. 左侧 tab 列表 → **Repositories**（带 Git 分支图标的文件夹）`,
        `3. 列表底部点 **+ Add repository**`,
        `4. 填 **URL**（例如 \`https://github.com/you/repo.git\`）和 **Description**`,
        `5. 在页面顶部点 **Save**`,
      ].join("\n"),
    });
  }

  if (q.team_size === "team") {
    tier3.push({
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "邀请同事加入",
      description: [
        `Multica 在小团队共享智能体的场景下最好用。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → **Settings**（⚙️，最底部）`,
        `2. 左侧 tab 列表 → **Members**（人形图标）`,
        `3. 点 **Add member** → 填邮箱 → 选角色 → **Send invite**`,
        ``,
        `[关于成员与角色 →](https://multica.ai/docs/zh/members-roles)`,
      ].join("\n"),
    });
  }

  tier3.push(
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "塑造智能体的 Instructions（创建之后再做）",
      description: [
        `**前置条件**：你至少有一个智能体。`,
        ``,
        `创建智能体只是开始。**Instructions tab** 才是塑造它行为的地方。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → **Agents**（🤖）`,
        `2. 在左侧列表点一个智能体`,
        `3. 右侧面板 → 点 **Instructions** tab（与 Skills / Environment / MCP / Custom Args 并列）`,
        `4. 编辑 markdown —— 自动保存`,
        ``,
        `Workspace Context 和智能体 Instructions 是叠加的——每个 task 都会同时带上。Instructions 写这个智能体特有的，Context 写整个工作区都适用的。`,
        ``,
        `如果这个智能体要使用 Claude Code MCP server，请在 **MCP** tab 里配置 JSON。非 Claude 运行时目前会忽略 MCP 配置。`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "实时观看智能体工作（分配 task 之后再做）",
      description: [
        `**了解性 task** —— 现在不用做什么，知道有这个东西就行。`,
        ``,
        `当智能体在某个 issue 上工作时，**Activity** 区顶部会出现一张 **Live card**（滚动时会粘在视口顶部）。`,
        ``,
        `Live card 实时展示智能体正在调用哪个工具、流式思考、当前状态。执行结束后，下方的 **Task Run History** 列出每一次运行 —— 点 **View transcript** 可以打开完整时间线。`,
        ``,
        `[关于执行任务 →](https://multica.ai/docs/zh/tasks)`,
      ].join("\n"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: "搭一个 Autopilot（有了智能体之后再做）",
      description: [
        `**前置条件**：你至少有一个智能体。`,
        ``,
        `**Autopilot** 把一段 prompt 变成定时 task。每天/每周/每小时自动建一个 issue 并分给智能体。`,
        ``,
        `**在哪**：`,
        `1. 侧边栏 → 在 **Workspace** 分组下点 **Autopilot**（⚡ 闪电图标）`,
        `2. 选一个模板，或者点 **+ New autopilot** 从空白开始`,
        `3. 填：**Name** / **Prompt** / **Agent** / **Schedule**（频率 + 时间 + 时区）→ **Create**`,
        ``,
        `[关于自动化 →](https://multica.ai/docs/zh/autopilots)`,
      ].join("\n"),
    },
  );

  return [...tier1, ...tier2, ...tier3];
}
