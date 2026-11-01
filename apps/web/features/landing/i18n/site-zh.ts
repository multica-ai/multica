import type { LandingDict } from "./types";

export const zhSiteCopy: Pick<LandingDict, "site" | "home"> = {
  site: {
    nav: {
      product: "产品",
      solutions: "解决方案",
      resources: "资源",
      customers: "客户",
      pricing: "价格",
      docs: "文档",
      worksWith: "支持的智能体",
      allAgents: "全部 27 款智能体",
      learn: "学习",
      documentation: "文档",
      changelog: "更新日志",
      useCases: "案例",
      blog: "博客",
      download: "下载",
      community: "社区",
      company: "公司",
      about: "关于我们",
      careers: "加入我们",
      press: "媒体与品牌",
      contactSales: "联系销售",
      trust: "信任与合规",
      security: "安全",
      licensing: "许可协议",
      privacy: "隐私政策",
    },
    header: {
      home: "Multica 首页",
      primary: "主导航",
      menu: "菜单",
      openMenu: "打开菜单",
      closeMenu: "关闭菜单",
      githubStars: "GitHub，{count} 颗星",
      logIn: "登录",
      getStarted: "开始使用",
      dashboard: "进入工作台",
    },
    footer: {
      tagline: "人和智能体一起工作的团队空间。",
      navigation: "页脚导航",
      language: "语言",
      copyright: "© {year} Multica. 保留所有权利。",
    },
    cta: {
      title: "今天就让第一个智能体开工",
      lead: "接入你已经在用的智能体，给它派第一个 Issue。",
      getStarted: "开始使用",
      bookDemo: "预约演示",
    },
  },
  home: {
    hero: {
      headlinePrefix: "让 ",
      headlineSuffix: " 成为你的队友。",
      lead: "你的团队和智能体共用一个工作空间：分派任务、跟进进度、审核结果，主动权始终在你手里。",
      getStarted: "开始使用",
      openDashboard: "进入工作台",
      bookDemo: "预约演示",
      workflowGroup: "工作流",
      workflows: {
        ship: {
          label: "交付一个功能",
          short: "交付",
          description:
            "一个由工作流驱动的 Multica 项目看板：Issue 依次经过 Spec、Build、Code review、Approve 和 Shipped，轮流交给规划智能体、编码智能体、审查智能体和一位同事",
        },
        launch: {
          label: "发布内容",
          short: "发布",
          description:
            "一个由工作流驱动的市场团队项目看板：一篇发布文章依次经过 Brief、Research、Draft、Edit 和 Published，轮流交给调研智能体、写作智能体和一位编辑",
        },
        triage: {
          label: "处理用户反馈",
          short: "反馈",
          description:
            "一个由工作流驱动的支持团队项目看板：Autopilot 把用户反馈归入 Inbox，Issue 再依次经过 Reproduce、Fix、Reply 和 Resolved，轮流交给分诊智能体、QA 智能体和编码智能体",
        },
      },
    },
    logoWall: { title: "这些公司的工程师在用" },
    stories: {
      title: "智能体加入团队后，会有什么不同",
      lead: "三个团队把智能体的工作搬进 Multica 之后的故事。",
      beforeLabel: "之前",
      afterLabel: "用 Multica",
      compare: "对比之前和使用 Multica 之后",
      compareValue: "之前占 {value}%",
      items: {
        board: {
          title: "三十个智能体，三十个标签页",
          outcomes: [
            { value: "30 个标签页", caption: "要逐个翻，才知道哪个智能体卡住了" },
            { value: "1 个收件箱", caption: "直接告诉你哪个智能体在等你" },
          ],
          before: "每个智能体各占一个终端标签页，它的提问就等在你没盯着的那一个里。",
          after: "所有工作都在一个看板上，需要你处理的事都会进收件箱。",
        },
        digest: {
          title: "没人有空做的那些工作",
          outcomes: [
            { value: "2%", caption: "客服对话有人看过" },
            { value: "100%", caption: "每晚由智能体全部看完" },
          ],
          before: "每天只有人抽查几十条客服对话，剩下的从来没人看。",
          after: "Autopilot 每晚把每一条都看完，把需要修的问题建成 Issue。第二天早上你看一眼摘要就行。",
        },
        pipeline: {
          title: "流程总在等人",
          outcomes: [
            { value: "9 天", caption: "修一个问题，其中 4 天在等待" },
            { value: "3 天", caption: "智能体交接，人只负责审批" },
          ],
          before: "修复的每一步，都要等有人发现轮到自己了。",
          after: "智能体把每一步交给下一位，人只在需要审批时出手。",
        },
      },
    },
    features: {
      title: "为人和智能体组成的团队而生",
      items: {
        runtimes: {
          title: "跑在你的机器上，也能跑在我们的云上",
          body: "一条命令接入笔记本或服务器，Multica 会自动找到上面装好的编码智能体。也可以直接在托管的云沙箱里运行。",
          alt: "运行时页面：列出两台机器和 Multica Cloud，以及每台上找到的编码工具",
        },
        workflows: {
          title: "工作流把每一步交接好",
          body: "给每个状态指定负责人。Issue 进入下一步，就自动交给这一步的智能体或同事。",
          alt: "“Ship a feature”工作流：Spec 交给 Lead Agent，Build 交给 Backend Agent，Code review 交给 QA Agent，Approve 交给 Mia Chen，最后到 Shipped；右侧打开了 Build 这一步的交接设置",
        },
        chat: {
          title: "在聊天里 @ 智能体",
          body: "在 Slack、飞书、钉钉、企业微信或 Telegram 里都可以。智能体会读完对话、建好 Issue，然后开工。",
          alt: "团队聊天频道：Mia 就导出失败的问题 @ 了 Backend Agent，智能体在频道里回复，附上它新建并正在处理的 Multica Issue 链接",
        },
        autopilots: {
          title: "Autopilot 按计划自动运行",
          body: "依赖升级、问题分诊、定期报告，每小时或每周一自动建好 Issue，交到智能体手里。",
          alt: "Autopilot 列表：五个定时任务，以及各自的智能体和下次运行时间",
        },
        skills: {
          title: "全队共享的技能",
          body: "团队做一件事的方法只要写一次，每个智能体都会照着做。",
          alt: "技能列表：团队写好的五个技能，以及用到它们的智能体",
        },
      },
    },
    agents: {
      lead: "不用学新的智能体。",
      title: "直接用你已经在用的。",
      all: "全部 27 款智能体",
    },
    updates: {
      eyebrow: "更新日志",
      title: "最近发布。",
      latest: "最新",
      more: "+{count} 项",
      all: "全部版本",
    },
  },
};
