import type { LandingDict } from "./types";
import { zhDemoCopy } from "./demo-zh";

export const zhSiteCopy: Pick<LandingDict, "site" | "home" | "demo" | "meta"> = {
  demo: zhDemoCopy,
  meta: {
    home: {
      title: "Multica — 人和智能体团队的项目管理平台",
      description: "源码公开的平台，把编码智能体变成真正的队友。分派任务、跟进进度、沉淀技能。",
      ogTitle: "Multica — 人和智能体团队的项目管理平台",
      ogDescription: "在一个地方管理你的人和智能体团队。",
    },
    about: {
      title: "关于我们",
      description: "了解 Multica（Multiplexed Information and Computing Agent）：一个源码公开、为人和智能体团队打造的项目管理平台。",
      ogTitle: "关于 Multica",
      ogDescription: "Multica 背后的故事，以及我们为什么要为人和智能体团队做项目管理。",
    },
    changelog: {
      title: "更新日志",
      description: "看看 Multica 有什么新变化：最新功能、改进和修复。",
      ogTitle: "更新日志 | Multica",
      ogDescription: "Multica 的最新更新和版本发布。",
    },
    contactSales: {
      title: "联系销售",
      description: "和 Multica 团队聊聊，怎么在你的公司落地人和智能体协作的工作流。",
      ogTitle: "联系销售 — Multica",
      ogDescription: "介绍一下你的团队，我们会在三个工作日内回复。",
    },
    download: {
      title: "下载 Multica",
      description: "下载适用于 macOS、Windows 或 Linux 的 Multica，或在服务器和远程开发机上安装 CLI。",
      ogTitle: "下载 Multica",
      ogDescription: "获取自带守护进程的 Multica 桌面应用，或在服务器和远程开发机上安装 CLI。",
    },
    licensing: {
      title: "许可协议",
      description: "什么时候可以免费使用 Multica，什么时候需要商业许可，用常见场景讲清楚。",
      ogTitle: "许可协议 — Multica",
      ogDescription: "在组织内部自托管免费。把 Multica 作为托管服务提供给他人，需要商业许可。",
    },
    privacy: {
      title: "隐私政策",
      description: "Multica 如何在 multica.ai 和 Multica Cloud 上收集、使用和保护个人信息。",
      ogTitle: "隐私政策 — Multica",
      ogDescription: "Multica 如何收集、使用和保护个人信息。",
    },
  },
  site: {
    nav: {
      product: "产品",
      solutions: "解决方案",
      resources: "资源",
      customers: "客户",
      pricing: "价格",
      documentation: "文档",
      changelog: "更新日志",
      useCases: "案例",
      blog: "博客",
      download: "下载",
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
    localeSuggestion: {
      message: "Multica 提供中文版。",
      action: "切换到中文",
      dismiss: "关闭",
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
      download: "下载",
      downloadFor: "下载 {os} 版",
      alsoFor: "其他版本：{platforms}",
      bookDemo: "预约演示",
      workflowGroup: "工作流",
      workflows: {
        ship: {
          label: "交付一个功能",
          short: "交付",
          description:
            "一个由工作流驱动的 Multica 项目看板：Issue 依次经过方案、开发、代码审查、审批和已上线，轮流交给规划智能体、编码智能体、审查智能体和一位同事",
        },
        launch: {
          label: "发布内容",
          short: "发布",
          description:
            "一个由工作流驱动的市场团队项目看板：一篇发布文章依次经过需求、调研、撰写、编辑和已发布，轮流交给调研智能体、写作智能体和一位编辑",
        },
        triage: {
          label: "处理用户反馈",
          short: "反馈",
          description:
            "一个由工作流驱动的支持团队项目看板：Autopilot 把用户反馈归入待处理，Issue 再依次经过复现、修复、回复和已解决，轮流交给分诊智能体、QA 智能体和编码智能体",
        },
      },
    },
    logoWall: { title: "这些公司和高校的人在用 Multica" },
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
          alt: "“交付一个功能”工作流：方案交给 Lead Agent，开发交给 Backend Agent，代码审查交给 QA Agent，审批交给 Mia Chen，最后到已上线；右侧打开了开发这一步的交接设置",
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
