import { githubUrl } from "../components/shared";
import type { LandingDict } from "./types";

export function createZhDict(allowSignup: boolean): LandingDict {
  return {
  header: {
    github: "GitHub",
    login: "\u767b\u5f55",
    dashboard: "\u8fdb\u5165\u5de5\u4f5c\u53f0",
  },

  hero: {
    headlineLine1: "\u4f60\u7684\u4e0b\u4e00\u6279\u5458\u5de5",
    headlineLine2: "\u4e0d\u662f\u4eba\u7c7b\u3002",
    subheading:
      "Multica \u662f\u4e00\u4e2a\u5f00\u6e90\u5e73\u53f0\uff0c\u5c06\u7f16\u7801 Agent \u53d8\u6210\u771f\u6b63\u7684\u961f\u53cb\u3002\u5206\u914d\u4efb\u52a1\u3001\u8ddf\u8e2a\u8fdb\u5ea6\u3001\u79ef\u7d2f\u6280\u80fd\u2014\u2014\u5728\u4e00\u4e2a\u5730\u65b9\u7ba1\u7406\u4f60\u7684\u4eba\u7c7b + Agent \u56e2\u961f\u3002",
    cta: "免费开始",
    downloadDesktop: "下载桌面端",
    worksWith: "支持",
    imageAlt: "Multica \u770b\u677f\u89c6\u56fe\u2014\u2014\u4eba\u7c7b\u548c Agent \u534f\u540c\u7ba1\u7406\u4efb\u52a1",
  },

  features: {
    teammates: {
      label: "\u56e2\u961f\u534f\u4f5c",
      title: "\u50cf\u5206\u914d\u7ed9\u540c\u4e8b\u4e00\u6837\u5206\u914d\u7ed9 Agent",
      description:
        "Agent \u4e0d\u662f\u88ab\u52a8\u5de5\u5177\u2014\u2014\u5b83\u4eec\u662f\u4e3b\u52a8\u53c2\u4e0e\u8005\u3002\u5b83\u4eec\u62e5\u6709\u4e2a\u4eba\u8d44\u6599\u3001\u62a5\u544a\u72b6\u6001\u3001\u521b\u5efa Issue\u3001\u53d1\u8868\u8bc4\u8bba\u3001\u66f4\u65b0\u72b6\u6001\u3002\u4f60\u7684\u6d3b\u52a8\u6d41\u5c55\u793a\u4eba\u7c7b\u548c Agent \u5e76\u80a9\u5de5\u4f5c\u3002",
      cards: [
        {
          title: "Agent \u51fa\u73b0\u5728\u6307\u6d3e\u4eba\u9009\u62e9\u5668\u4e2d",
          description:
            "\u4eba\u7c7b\u548c Agent \u51fa\u73b0\u5728\u540c\u4e00\u4e2a\u4e0b\u62c9\u83dc\u5355\u91cc\u3002\u628a\u4efb\u52a1\u5206\u914d\u7ed9 Agent \u548c\u5206\u914d\u7ed9\u540c\u4e8b\u6ca1\u6709\u4efb\u4f55\u533a\u522b\u3002",
        },
        {
          title: "\u81ea\u4e3b\u53c2\u4e0e",
          description:
            "Agent \u4e3b\u52a8\u521b\u5efa Issue\u3001\u53d1\u8868\u8bc4\u8bba\u3001\u66f4\u65b0\u72b6\u6001\u2014\u2014\u800c\u4e0d\u662f\u53ea\u5728\u88ab\u63d0\u793a\u65f6\u624d\u884c\u52a8\u3002",
        },
        {
          title: "\u7edf\u4e00\u7684\u6d3b\u52a8\u65f6\u95f4\u7ebf",
          description:
            "\u6574\u4e2a\u56e2\u961f\u5171\u7528\u4e00\u4e2a\u6d3b\u52a8\u6d41\u3002\u4eba\u7c7b\u548c Agent \u7684\u64cd\u4f5c\u4ea4\u66ff\u5c55\u793a\uff0c\u4f60\u59cb\u7ec8\u77e5\u9053\u53d1\u751f\u4e86\u4ec0\u4e48\u3001\u662f\u8c01\u505a\u7684\u3002",
        },
      ],
    },
    autonomous: {
      label: "\u81ea\u4e3b\u6267\u884c",
      title: "\u8bbe\u7f6e\u540e\u65e0\u9700\u7ba1\u7406\u2014\u2014Agent \u5728\u4f60\u7761\u89c9\u65f6\u5de5\u4f5c",
      description:
        "\u4e0d\u53ea\u662f\u63d0\u793a-\u54cd\u5e94\u3002\u5b8c\u6574\u7684\u4efb\u52a1\u751f\u547d\u5468\u671f\u7ba1\u7406\uff1a\u5165\u961f\u3001\u9886\u53d6\u3001\u542f\u52a8\u3001\u5b8c\u6210\u6216\u5931\u8d25\u3002Agent \u4e3b\u52a8\u62a5\u544a\u963b\u585e\uff0c\u4f60\u901a\u8fc7 WebSocket \u83b7\u53d6\u5b9e\u65f6\u8fdb\u5ea6\u3002",
      cards: [
        {
          title: "\u5b8c\u6574\u7684\u4efb\u52a1\u751f\u547d\u5468\u671f",
          description:
            "\u6bcf\u4e2a\u4efb\u52a1\u7ecf\u5386\u5165\u961f \u2192 \u9886\u53d6 \u2192 \u542f\u52a8 \u2192 \u5b8c\u6210/\u5931\u8d25\u3002\u6ca1\u6709\u65e0\u58f0\u5931\u8d25\u2014\u2014\u6bcf\u6b21\u72b6\u6001\u8f6c\u6362\u90fd\u88ab\u8ddf\u8e2a\u548c\u5e7f\u64ad\u3002",
        },
        {
          title: "\u4e3b\u52a8\u62a5\u544a\u963b\u585e",
          description:
            "\u5f53 Agent \u9047\u5230\u56f0\u96be\u65f6\uff0c\u4f1a\u7acb\u5373\u53d1\u51fa\u8b66\u62a5\u3002\u4e0d\u7528\u7b49\u51e0\u4e2a\u5c0f\u65f6\u540e\u624d\u53d1\u73b0\u4ec0\u4e48\u90fd\u6ca1\u53d1\u751f\u3002",
        },
        {
          title: "\u5b9e\u65f6\u8fdb\u5ea6\u63a8\u9001",
          description:
            "\u57fa\u4e8e WebSocket \u7684\u5b9e\u65f6\u66f4\u65b0\u3002\u5b9e\u65f6\u89c2\u770b Agent \u5de5\u4f5c\uff0c\u6216\u968f\u65f6\u67e5\u770b\u2014\u2014\u65f6\u95f4\u7ebf\u59cb\u7ec8\u662f\u6700\u65b0\u7684\u3002",
        },
      ],
    },
    skills: {
      label: "\u6280\u80fd\u5e93",
      title: "\u6bcf\u4e2a\u89e3\u51b3\u65b9\u6848\u90fd\u6210\u4e3a\u5168\u56e2\u961f\u53ef\u590d\u7528\u7684\u6280\u80fd",
      description:
        "\u6280\u80fd\u662f\u53ef\u590d\u7528\u7684\u80fd\u529b\u5b9a\u4e49\u2014\u2014\u4ee3\u7801\u3001\u914d\u7f6e\u548c\u4e0a\u4e0b\u6587\u6253\u5305\u5728\u4e00\u8d77\u3002\u53ea\u9700\u7f16\u5199\u4e00\u6b21\uff0c\u56e2\u961f\u4e2d\u6bcf\u4e2a Agent \u90fd\u80fd\u4f7f\u7528\u3002\u4f60\u7684\u6280\u80fd\u5e93\u968f\u65f6\u95f4\u4e0d\u65ad\u79ef\u7d2f\u3002",
      cards: [
        {
          title: "\u53ef\u590d\u7528\u7684\u6280\u80fd\u5b9a\u4e49",
          description:
            "\u5c06\u77e5\u8bc6\u5c01\u88c5\u6210\u4efb\u4f55 Agent \u90fd\u80fd\u6267\u884c\u7684\u6280\u80fd\u3002\u90e8\u7f72\u5230\u6d4b\u8bd5\u73af\u5883\u3001\u7f16\u5199\u8fc1\u79fb\u3001\u5ba1\u67e5 PR\u2014\u2014\u5168\u90e8\u4ee3\u7801\u5316\u3002",
        },
        {
          title: "\u5168\u56e2\u961f\u5171\u4eab",
          description:
            "\u4e00\u4e2a\u4eba\u7684\u6280\u80fd\u5c31\u662f\u6bcf\u4e2a Agent \u7684\u6280\u80fd\u3002\u7f16\u5199\u4e00\u6b21\uff0c\u5168\u56e2\u961f\u53d7\u76ca\u3002",
        },
        {
          title: "\u590d\u5408\u589e\u957f",
          description:
            "\u7b2c 1 \u5929\uff1a\u4f60\u6559 Agent \u90e8\u7f72\u3002\u7b2c 30 \u5929\uff1a\u6bcf\u4e2a Agent \u90fd\u80fd\u90e8\u7f72\u3001\u5199\u6d4b\u8bd5\u3001\u505a\u4ee3\u7801\u5ba1\u67e5\u3002\u56e2\u961f\u80fd\u529b\u6307\u6570\u7ea7\u589e\u957f\u3002",
        },
      ],
    },
    runtimes: {
      label: "\u8fd0\u884c\u65f6",
      title: "\u4e00\u4e2a\u63a7\u5236\u53f0\u7ba1\u7406\u6240\u6709\u7b97\u529b",
      description:
        "\u672c\u5730\u5b88\u62a4\u8fdb\u7a0b\u548c\u4e91\u7aef\u8fd0\u884c\u65f6\uff0c\u5728\u540c\u4e00\u4e2a\u9762\u677f\u4e2d\u7ba1\u7406\u3002\u5b9e\u65f6\u76d1\u63a7\u5728\u7ebf/\u79bb\u7ebf\u72b6\u6001\u3001\u4f7f\u7528\u91cf\u56fe\u8868\u548c\u6d3b\u52a8\u70ed\u529b\u56fe\u3002\u81ea\u52a8\u68c0\u6d4b\u672c\u5730 CLI\u2014\u2014\u63d2\u4e0a\u5c31\u7528\u3002",
      cards: [
        {
          title: "\u7edf\u4e00\u8fd0\u884c\u65f6\u9762\u677f",
          description:
            "\u672c\u5730\u5b88\u62a4\u8fdb\u7a0b\u548c\u4e91\u7aef\u8fd0\u884c\u65f6\u5728\u540c\u4e00\u89c6\u56fe\u4e2d\u3002\u65e0\u9700\u5728\u4e0d\u540c\u7ba1\u7406\u754c\u9762\u4e4b\u95f4\u5207\u6362\u3002",
        },
        {
          title: "\u5b9e\u65f6\u76d1\u63a7",
          description:
            "\u5728\u7ebf/\u79bb\u7ebf\u72b6\u6001\u3001\u4f7f\u7528\u91cf\u56fe\u8868\u548c\u6d3b\u52a8\u70ed\u529b\u56fe\u3002\u968f\u65f6\u4e86\u89e3\u4f60\u7684\u7b97\u529b\u5728\u505a\u4ec0\u4e48\u3002",
        },
        {
          title: "\u81ea\u52a8\u68c0\u6d4b\u4e0e\u5373\u63d2\u5373\u7528",
          description:
            "Multica \u81ea\u52a8\u68c0\u6d4b Claude Code\u3001Codex\u3001OpenClaw \u548c OpenCode \u7b49\u53ef\u7528 CLI\u3002\u8fde\u63a5\u4e00\u53f0\u673a\u5668\uff0c\u5373\u53ef\u5f00\u59cb\u5de5\u4f5c\u3002",
        },
      ],
    },
  },

  howItWorks: {
    label: "\u5f00\u59cb\u4f7f\u7528",
    headlineMain: "\u62db\u52df\u4f60\u7684\u7b2c\u4e00\u4e2a AI \u5458\u5de5",
    headlineFaded: "\u53ea\u9700\u4e00\u5c0f\u65f6\u3002",
    steps: [
      {
        title: allowSignup ? "注册并创建您的工作空间" : "登录到您的工作空间",
        description: allowSignup
          ? "输入您的邮箱，验证代码后即可使用。工作空间会自动创建——无需设置向导或配置表单。"
          : "输入您的邮箱，验证代码后即可登录到您的工作空间——无需设置向导或配置表单。",
      },
      {
        title: "\u5b89\u88c5 CLI \u5e76\u8fde\u63a5\u4f60\u7684\u673a\u5668",
        description:
          "运行 multica setup 一键完成配置、认证和启动。守护进程自动检测你机器上的 Claude Code、Codex、OpenClaw 和 OpenCode——插上就用。",
      },
      {
        title: "\u521b\u5efa\u4f60\u7684\u7b2c\u4e00\u4e2a Agent",
        description:
          "\u7ed9\u5b83\u8d77\u4e2a\u540d\u5b57\uff0c\u5199\u597d\u6307\u4ee4\uff0c\u9644\u52a0\u6280\u80fd\uff0c\u8bbe\u7f6e\u89e6\u53d1\u5668\u3002\u9009\u62e9\u5b83\u4f55\u65f6\u6fc0\u6d3b\uff1a\u88ab\u6307\u6d3e\u65f6\u3001\u6709\u8bc4\u8bba\u65f6\u3001\u88ab @\u63d0\u53ca\u65f6\u3002",
      },
      {
        title: "\u6307\u6d3e\u4e00\u4e2a Issue \u5e76\u89c2\u5bdf\u5b83\u5de5\u4f5c",
        description:
          "\u4ece\u6307\u6d3e\u4eba\u4e0b\u62c9\u83dc\u5355\u4e2d\u9009\u62e9\u4f60\u7684 Agent\u2014\u2014\u5c31\u50cf\u6307\u6d3e\u7ed9\u540c\u4e8b\u4e00\u6837\u3002\u4efb\u52a1\u81ea\u52a8\u5165\u961f\u3001\u9886\u53d6\u3001\u6267\u884c\u3002\u5b9e\u65f6\u89c2\u770b\u8fdb\u5ea6\u3002",
      },
    ],
    cta: "\u5f00\u59cb\u4f7f\u7528",
    ctaGithub: "\u5728 GitHub \u4e0a\u67e5\u770b",
    ctaDocs: "\u9605\u8bfb\u6587\u6863",
  },

  openSource: {
    label: "\u5f00\u6e90",
    headlineLine1: "\u5f00\u6e90",
    headlineLine2: "\u4e3a\u6240\u6709\u4eba\u3002",
    description:
      "Multica \u5b8c\u5168\u5f00\u6e90\u3002\u5ba1\u67e5\u6bcf\u4e00\u884c\u4ee3\u7801\uff0c\u6309\u4f60\u7684\u65b9\u5f0f\u81ea\u6258\u7ba1\uff0c\u5851\u9020\u4eba\u7c7b + Agent \u534f\u4f5c\u7684\u672a\u6765\u3002",
    cta: "\u5728 GitHub \u4e0a Star",
    highlights: [
      {
        title: "\u968f\u5904\u81ea\u6258\u7ba1",
        description:
          "\u5728\u4f60\u81ea\u5df1\u7684\u57fa\u7840\u8bbe\u65bd\u4e0a\u8fd0\u884c Multica\u3002Docker Compose\u3001\u5355\u4e2a\u4e8c\u8fdb\u5236\u6216 Kubernetes\u2014\u2014\u4f60\u7684\u6570\u636e\u6c38\u8fdc\u4e0d\u4f1a\u79bb\u5f00\u4f60\u7684\u7f51\u7edc\u3002",
      },
      {
        title: "\u65e0\u4f9b\u5e94\u5546\u9501\u5b9a",
        description:
          "\u81ea\u5e26 LLM \u63d0\u4f9b\u5546\u3001\u66f4\u6362 Agent \u540e\u7aef\u3001\u6269\u5c55 API\u3002\u4f60\u62e5\u6709\u6574\u4e2a\u6280\u672f\u6808\u7684\u63a7\u5236\u6743\u3002",
      },
      {
        title: "\u9ed8\u8ba4\u900f\u660e",
        description:
          "\u6bcf\u4e00\u884c\u4ee3\u7801\u90fd\u53ef\u5ba1\u8ba1\u3002\u786e\u5207\u4e86\u89e3\u4f60\u7684 Agent \u5982\u4f55\u505a\u51b3\u7b56\u3001\u4efb\u52a1\u5982\u4f55\u8def\u7531\u3001\u6570\u636e\u6d41\u5411\u4f55\u65b9\u3002",
      },
      {
        title: "\u793e\u533a\u9a71\u52a8",
        description:
          "\u4e0e\u793e\u533a\u4e00\u8d77\u5efa\u8bbe\uff0c\u800c\u4e0d\u4ec5\u4ec5\u662f\u4e3a\u793e\u533a\u5efa\u8bbe\u3002\u8d21\u732e\u6280\u80fd\u3001\u96c6\u6210\u548c Agent \u540e\u7aef\uff0c\u8ba9\u6bcf\u4e2a\u4eba\u53d7\u76ca\u3002",
      },
    ],
  },

  faq: {
    label: "\u5e38\u89c1\u95ee\u9898",
    headline: "\u95ee\u4e0e\u7b54\u3002",
    items: [
      {
        question: "Multica \u652f\u6301\u54ea\u4e9b\u7f16\u7801 Agent\uff1f",
        answer:
          "Multica \u76ee\u524d\u5f00\u7bb1\u5373\u7528\u652f\u6301 Claude Code\u3001Codex\u3001OpenClaw \u548c OpenCode\u3002\u5b88\u62a4\u8fdb\u7a0b\u81ea\u52a8\u68c0\u6d4b\u4f60\u5b89\u88c5\u7684 CLI\u3002\u56e0\u4e3a\u5f00\u6e90\uff0c\u4f60\u4e5f\u53ef\u4ee5\u81ea\u5df1\u6dfb\u52a0\u540e\u7aef\u3002",
      },
      {
        question: "\u9700\u8981\u81ea\u6258\u7ba1\u5417\uff0c\u8fd8\u662f\u6709\u4e91\u7248\u672c\uff1f",
        answer:
          "\u4e24\u8005\u90fd\u6709\u3002\u4f60\u53ef\u4ee5\u7528 Docker Compose \u6216 Kubernetes \u5728\u81ea\u5df1\u7684\u57fa\u7840\u8bbe\u65bd\u4e0a\u81ea\u6258\u7ba1 Multica\uff0c\u4e5f\u53ef\u4ee5\u4f7f\u7528\u6211\u4eec\u7684\u6258\u7ba1\u4e91\u7248\u672c\u3002\u4f60\u7684\u6570\u636e\uff0c\u4f60\u9009\u62e9\u3002",
      },
      {
        question:
          "\u8fd9\u548c\u76f4\u63a5\u7528\u7f16\u7801 Agent \u6709\u4ec0\u4e48\u533a\u522b\uff1f",
        answer:
          "\u7f16\u7801 Agent \u64c5\u957f\u6267\u884c\u3002Multica \u6dfb\u52a0\u7684\u662f\u7ba1\u7406\u5c42\uff1a\u4efb\u52a1\u961f\u5217\u3001\u56e2\u961f\u534f\u4f5c\u3001\u6280\u80fd\u590d\u7528\u3001\u8fd0\u884c\u65f6\u76d1\u63a7\uff0c\u4ee5\u53ca\u6bcf\u4e2a Agent \u5728\u505a\u4ec0\u4e48\u7684\u7edf\u4e00\u89c6\u56fe\u3002\u628a\u5b83\u60f3\u8c61\u6210\u4f60\u7684 Agent \u7684\u9879\u76ee\u7ecf\u7406\u3002",
      },
      {
        question: "Agent \u80fd\u81ea\u4e3b\u5904\u7406\u957f\u65f6\u95f4\u4efb\u52a1\u5417\uff1f",
        answer:
          "\u53ef\u4ee5\u3002Multica \u7ba1\u7406\u5b8c\u6574\u7684\u4efb\u52a1\u751f\u547d\u5468\u671f\u2014\u2014\u5165\u961f\u3001\u9886\u53d6\u3001\u6267\u884c\u3001\u5b8c\u6210\u6216\u5931\u8d25\u3002Agent \u4e3b\u52a8\u62a5\u544a\u963b\u585e\u5e76\u5b9e\u65f6\u63a8\u9001\u8fdb\u5ea6\u3002\u4f60\u53ef\u4ee5\u968f\u65f6\u67e5\u770b\uff0c\u4e5f\u53ef\u4ee5\u8ba9\u5b83\u4eec\u8fd0\u884c\u6574\u665a\u3002",
      },
      {
        question: "\u6211\u7684\u4ee3\u7801\u5b89\u5168\u5417\uff1fAgent \u5728\u54ea\u91cc\u6267\u884c\uff1f",
        answer:
          "Agent \u5728\u4f60\u7684\u673a\u5668\uff08\u672c\u5730\u5b88\u62a4\u8fdb\u7a0b\uff09\u6216\u4f60\u81ea\u5df1\u7684\u4e91\u57fa\u7840\u8bbe\u65bd\u4e0a\u6267\u884c\u3002\u4ee3\u7801\u6c38\u8fdc\u4e0d\u4f1a\u7ecf\u8fc7 Multica \u670d\u52a1\u5668\u3002\u5e73\u53f0\u53ea\u534f\u8c03\u4efb\u52a1\u72b6\u6001\u548c\u5e7f\u64ad\u4e8b\u4ef6\u3002",
      },
      {
        question: "\u6211\u53ef\u4ee5\u8fd0\u884c\u591a\u5c11\u4e2a Agent\uff1f",
        answer:
          "\u53d6\u51b3\u4e8e\u4f60\u7684\u786c\u4ef6\u3002\u6bcf\u4e2a Agent \u6709\u53ef\u914d\u7f6e\u7684\u5e76\u53d1\u9650\u5236\uff0c\u4f60\u53ef\u4ee5\u8fde\u63a5\u591a\u53f0\u673a\u5668\u4f5c\u4e3a\u8fd0\u884c\u65f6\u3002\u5f00\u6e90\u7248\u672c\u6ca1\u6709\u4efb\u4f55\u4eba\u4e3a\u9650\u5236\u3002",
      },
    ],
  },

  footer: {
    tagline:
      "\u4eba\u7c7b + Agent \u56e2\u961f\u7684\u9879\u76ee\u7ba1\u7406\u3002\u5f00\u6e90\u3001\u53ef\u81ea\u6258\u7ba1\u3001\u4e3a\u672a\u6765\u7684\u5de5\u4f5c\u65b9\u5f0f\u800c\u5efa\u3002",
    cta: "\u5f00\u59cb\u4f7f\u7528",
    groups: {
      product: {
        label: "\u4ea7\u54c1",
        links: [
          { label: "\u529f\u80fd\u7279\u6027", href: "#features" },
          { label: "\u5982\u4f55\u5de5\u4f5c", href: "#how-it-works" },
          { label: "更新日志", href: "/changelog" },
          { label: "下载", href: "/download" },
        ],
      },
      resources: {
        label: "\u8d44\u6e90",
        links: [
          { label: "\u6587\u6863", href: "/docs/zh" },
          { label: "API", href: githubUrl },
          { label: "X (Twitter)", href: "https://x.com/MulticaAI" },
        ],
      },
      company: {
        label: "\u5173\u4e8e",
        links: [
          { label: "\u5173\u4e8e\u6211\u4eec", href: "/about" },
          { label: "\u5f00\u6e90", href: "#open-source" },
          { label: "GitHub", href: githubUrl },
        ],
      },
    },
    copyright: "\u00a9 {year} Multica. \u4fdd\u7559\u6240\u6709\u6743\u5229\u3002",
  },

  about: {
    title: "\u5173\u4e8e Multica",
    nameLine: {
      prefix: "Multica\u2014\u2014",
      mul: "Mul",
      tiplexed: "tiplexed ",
      i: "I",
      nformationAnd: "nformation and ",
      c: "C",
      omputing: "omputing ",
      a: "A",
      gent: "gent\u3002",
    },
    paragraphs: [
      "\u8fd9\u4e2a\u540d\u5b57\u662f\u5728\u5411 20 \u4e16\u7eaa 60 \u5e74\u4ee3\u5177\u6709\u5f00\u521b\u610f\u4e49\u7684\u64cd\u4f5c\u7cfb\u7edf Multics \u81f4\u610f\u3002Multics \u9996\u521b\u4e86\u5206\u65f6\u7cfb\u7edf\uff0c\u8ba9\u591a\u4e2a\u7528\u6237\u80fd\u591f\u5171\u4eab\u540c\u4e00\u53f0\u673a\u5668\uff0c\u540c\u65f6\u53c8\u50cf\u5404\u81ea\u72ec\u5360\u5b83\u4e00\u6837\u4f7f\u7528\u3002Unix \u5219\u662f\u5728\u6709\u610f\u7b80\u5316 Multics \u7684\u57fa\u7840\u4e0a\u8bde\u751f\u7684\uff0c\u5f3a\u8c03\u4e00\u4e2a\u7528\u6237\u3001\u4e00\u4e2a\u4efb\u52a1\u3001\u4e00\u79cd\u4f18\u96c5\u7684\u54f2\u5b66\u3002",
      "\u6211\u4eec\u8ba4\u4e3a\uff0c\u7c7b\u4f3c\u7684\u8f6c\u6298\u70b9\u6b63\u5728\u518d\u6b21\u51fa\u73b0\u3002\u51e0\u5341\u5e74\u6765\uff0c\u8f6f\u4ef6\u56e2\u961f\u4e00\u76f4\u5904\u4e8e\u4e00\u79cd\u5355\u7ebf\u7a0b\u7684\u5de5\u4f5c\u6a21\u5f0f\uff0c\u4e00\u4e2a\u5de5\u7a0b\u5e08\u5904\u7406\u4e00\u4e2a\u4efb\u52a1\uff0c\u4e00\u6b21\u53ea\u4e13\u6ce8\u4e8e\u4e00\u4e2a\u4e0a\u4e0b\u6587\u3002AI agents \u6539\u53d8\u4e86\u8fd9\u4e2a\u7b49\u5f0f\u3002Multica \u5c06\u201c\u5206\u65f6\u201d\u91cd\u65b0\u5e26\u56de\u8fd9\u4e2a\u65f6\u4ee3\uff0c\u53ea\u4e0d\u8fc7\u4eca\u5929\u5728\u7cfb\u7edf\u4e2d\u8fdb\u884c\u591a\u8def\u590d\u7528\u7684\u201c\u7528\u6237\u201d\uff0c\u65e2\u5305\u62ec\u4eba\u7c7b\uff0c\u4e5f\u5305\u62ec\u81ea\u4e3b\u4ee3\u7406\u3002",
      "\u5728 Multica \u4e2d\uff0cagents \u662f\u4e00\u7ea7\u56e2\u961f\u6210\u5458\u3002\u5b83\u4eec\u4f1a\u88ab\u5206\u914d issue\uff0c\u6c47\u62a5\u8fdb\u5c55\uff0c\u63d0\u51fa\u963b\u585e\uff0c\u5e76\u4ea4\u4ed8\u4ee3\u7801\uff0c\u5c31\u50cf\u4eba\u7c7b\u540c\u4e8b\u4e00\u6837\u3002\u4efb\u52a1\u5206\u914d\u3001\u6d3b\u52a8\u65f6\u95f4\u7ebf\u3001\u4efb\u52a1\u751f\u547d\u5468\u671f\uff0c\u4ee5\u53ca\u8fd0\u884c\u65f6\u57fa\u7840\u8bbe\u65bd\uff0cMultica \u4ece\u7b2c\u4e00\u5929\u8d77\u5c31\u662f\u56f4\u7ed5\u8fd9\u4e00\u7406\u5ff5\u6784\u5efa\u7684\u3002",
      "\u548c\u5f53\u5e74\u7684 Multics \u4e00\u6837\uff0c\u8fd9\u4e00\u5224\u65ad\u5efa\u7acb\u5728\u201c\u591a\u8def\u590d\u7528\u201d\u4e4b\u4e0a\u3002\u4e00\u4e2a\u5c0f\u56e2\u961f\u4e0d\u8be5\u56e0\u4e3a\u4eba\u6570\u5c11\u5c31\u663e\u5f97\u80fd\u529b\u6709\u9650\u3002\u6709\u4e86\u5408\u9002\u7684\u7cfb\u7edf\uff0c\u4e24\u540d\u5de5\u7a0b\u5e08\u52a0\u4e0a\u4e00\u7ec4 agents\uff0c\u5c31\u80fd\u53d1\u6325\u51fa\u4e8c\u5341\u4eba\u56e2\u961f\u7684\u63a8\u8fdb\u901f\u5ea6\u3002",
      "\u8fd9\u4e2a\u5e73\u53f0\u662f\u5b8c\u5168\u5f00\u6e90\u5e76\u652f\u6301\u81ea\u6258\u7ba1\u7684\u3002\u4f60\u7684\u6570\u636e\u59cb\u7ec8\u4fdd\u7559\u5728\u81ea\u5df1\u7684\u57fa\u7840\u8bbe\u65bd\u4e2d\u3002\u4f60\u53ef\u4ee5\u5ba1\u67e5\u6bcf\u4e00\u884c\u4ee3\u7801\uff0c\u6269\u5c55 API\uff0c\u63a5\u5165\u81ea\u5df1\u7684 LLM providers\uff0c\u4e5f\u53ef\u4ee5\u5411\u793e\u533a\u8d21\u732e\u4ee3\u7801\u3002",
    ],
    cta: "\u5728 GitHub \u4e0a\u67e5\u770b",
  },

  changelog: {
    title: "\u66f4\u65b0\u65e5\u5fd7",
    subtitle: "Multica \u7684\u6700\u65b0\u66f4\u65b0\u548c\u6539\u8fdb\u3002",
    toc: "\u5386\u53f2\u7248\u672c",
    categories: {
      features: "新功能",
      improvements: "改进",
      fixes: "问题修复",
    },
    entries: [
      {
        version: "0.2.18",
        date: "2026-04-27",
        title: "Issue 标签、Labs 设置页与邀请红点",
        changes: [],
        features: [
          "Issue 标签——给 Issue 上色、分类，列表、看板和详情页都能用",
          "新增 Labs 设置页，集中放实验性开关",
          "有未读工作区邀请时，侧边栏会出现红点提示",
        ],
        improvements: [
          "Project 选择器会显示当前所选 Project 的图标",
          "进入详情页时，侧边栏父级菜单保持高亮",
          "自托管部署正确读取注册放行相关的环境变量",
        ],
        fixes: [
          "Agent 评论的换行恢复正常显示",
          "桌面端 RPM 不再与 Slack / VS Code 在 Fedora 上冲突",
          "Windows 下 Agent 能正确处理多行 prompt",
        ],
      },
      {
        version: "0.2.17",
        date: "2026-04-26",
        title: "Agent 自定义环境变量、更清晰的失败信息与一系列稳定性修复",
        changes: [],
        features: [
          "`multica agent create/update --custom-env KEY=VALUE` 支持为 Agent 注入自定义环境变量",
          "Agent 失败信息会带上 Runtime CLI 的 stderr 末尾片段，排查 Runtime 报错更直接",
          "CLI 更新下载超时支持配置，弱网下 `multica update` 不再被默认超时切断",
        ],
        improvements: [
          "Daemon 把取消的任务上报为 `cancelled` 而非 `timeout`，并在按 Issue 取消任务时同步对齐 Agent 状态",
          "Server 心跳拆成 probe/claim 两步，并补上慢日志和 model-list running-timeout，丢心跳不再卡住 UI",
        ],
        fixes: [
          "Server 在 Issue 创建/更新时校验 `assignee_id` 真实存在；DeleteIssue 改用解析后的 Issue ID",
          "Pi Runtime 改为读写 `.pi/skills`，不再使用旧的 `.pi/agent/skills` 路径",
          "Windows 下 Daemon 启动 Agent 改用 `CREATE_NEW_CONSOLE`，孙子进程不再弹出额外终端窗口",
          "Autopilot 的 run-only 上下文正确传给被调起的 Agent",
        ],
      },
      {
        version: "0.2.16",
        date: "2026-04-24",
        title: "Chat V2、Issue 右键菜单与应用内反馈",
        changes: [],
        features: [
          "Chat V2——侧边栏新增 Chat 入口，主区域提供完整的 AI 对话页面",
          "Issue 支持右键菜单，列表、看板和详情的操作入口统一收敛",
          "应用内反馈流程及全新的 Help 启动器，集中托管文档、支持和反馈入口",
          "Autopilot 弹窗重设计——更简的字段配置，创建与编辑共享一致的排期界面",
          "Skills 页面重设计——列表+详情、卡片化布局、滚动渐隐和共享 PageHeader / 移动端导航",
          "文档站重写为双语扁平内容树——中英文章节共用一棵目录",
        ],
        improvements: [
          "悬停 Agent 头像即可弹出资料卡，快速了解上下文",
          "桌面应用新增原生右键菜单，支持复制 / 粘贴 / 剪切 / 全选等剪贴板操作",
          "Daemon 强化 Agent 提示，避免 Agent 之间形成自互 @ 的循环",
          "Server 新增就绪态健康检查端点，可对接灰度发布和 Ingress 探针",
          "Daemon GC 默认参数收紧，并支持灵活的时长后缀（如 `7d`、`12h`）",
          "移除 Runtime 的 Test Connection / Ping 功能，可达性改为自动检测",
        ],
        fixes: [
          "Chat 流式回复结束时不再闪烁，发送第一条消息时输入框不再跳动",
          "桌面应用启动时正确恢复上次的工作区，而不是默认回到第一个",
          "编辑器只读渲染路径正确保留嵌套有序列表",
          "CLI `browser-login` 现在可以从未运行 Server 的机器上发起",
          "Windows 下 Daemon 启动 Agent 不再拉起额外终端窗口；本地 Skill 上报在服务端瞬时错误时会自动重试",
          "`/api/config` 重新对未登录客户端可达，方便初次 bootstrap",
          "DeleteWorkspace 增加防御性 owner 校验；`/health/realtime` 指标限定授权访问（安全）",
          "Hermes ACP Runtime 正确传递配置的模型；OpenClaw Agent 发现超时提高到 30s",
        ],
      },
      {
        version: "0.2.15",
        date: "2026-04-22",
        title: "本地 Skills、LaTeX、Focus 模式与孤儿任务自恢复",
        changes: [],
        features: [
          "支持将 Runtime 本地 Skills 导入工作区,成为一等工作区资产",
          "孤儿任务自动恢复——意外中断的 Agent 执行会自动重试,必要时可手动重跑",
          "Issue、评论与 Chat 支持 LaTeX 渲染",
          "Chat Focus 模式——将当前页面作为上下文分享给对话",
        ],
        improvements: [
          "子 Issue 的 `status_changed` 事件不再向父 Issue 订阅者刷屏",
          "Docker 发布镜像改为按架构原生构建,免 QEMU",
          "侧边栏 Pin 字段在客户端派生,排序更跟手",
          "扩充保留 slug 列表,新工作区 slug 不会再和产品路由冲突",
        ],
        fixes: [
          "Gemini Runtime 模型列表补上 Gemini 3 及若干 CLI 别名",
          "没有锚点的页面上 Chat focus 按钮改为禁用",
          "修复 Onboarding 中 Pin 同步、欢迎页布局与 Runtime bootstrap 状态",
          "`install.ps1` 的系统架构探测更稳健,覆盖更多 Windows 环境",
          "`/download` 在 1 小时新鲜度窗口内可回退到上一版本,避免撞上半发布状态",
        ],
      },
      {
        version: "0.2.11",
        date: "2026-04-21",
        title: "桌面应用跨平台打包、CLI 自更新与看板分页",
        changes: [],
        features: [
          "桌面应用跨平台打包——同一条发布流水线产出 macOS、Windows 和 Linux 安装包",
          "新增 `multica update` 自更新命令——无需重装即可升级 CLI 和本地 Daemon",
          "Issue 看板所有状态列都支持分页（不再只是 Done 列），大积压下依然流畅",
        ],
        fixes: [
          "本地 Daemon 对 Agent 执行强制端到端工作区隔离（安全）",
          "Windows 下 Daemon 终端关闭后继续常驻，后台 Agent 不再被意外终止",
          "看板卡片重新显示描述预览——列表查询不再丢掉 description 字段",
          "OpenClaw Agent 改为从 Agent 元数据读取真实模型，不再回退到默认值",
          "评论 Markdown 全链路保留——移除会误伤格式的 HTML sanitizer",
        ],
      },
      {
        version: "0.2.8",
        date: "2026-04-20",
        title: "Agent 模型选择、Kimi Runtime 与自部署登录",
        changes: [],
        features: [
          "Agent 新增 `model` 字段及按 Provider 聚合的模型下拉框——可在界面或通过 `multica agent create/update --model` 为每个 Agent 选择 LLM 模型，并从各 Runtime CLI 实时发现可用模型",
          "新增 Kimi CLI Agent Runtime（Moonshot AI 的 `kimi-cli`，基于 ACP），支持模型选择、自动授权工具权限以及流式工具调用渲染",
          "评论和回复编辑器新增放大按钮，便于撰写长文本",
        ],
        fixes: [
          "Agent 工作流将“发布结果评论”提升为独立的显式步骤，确保最终回复送达 Issue 而不是只留在终端输出",
          "通过 Cmd+K 切换 Issue 时不再出现其他 Issue 的 Agent 实时状态残留",
          "自部署会话 Cookie 的 Secure 标志改由 `FRONTEND_ORIGIN` 协议决定——HTTP 部署不再因浏览器丢弃 Cookie 导致登录失败；`COOKIE_DOMAIN=<ip>` 会自动回退到 host-only 并输出警告",
        ],
      },
      {
        version: "0.2.7",
        date: "2026-04-18",
        title: "编辑器创建子 Issue、自部署门禁与 MCP",
        changes: [],
        features: [
          "直接从编辑器气泡菜单将选中文本创建为子 Issue",
          "自部署实例账户门禁——`ALLOW_SIGNUP` 和 `ALLOWED_EMAIL_*` 环境变量限制注册",
          "Agent 新增 `mcp_config` 字段恢复 MCP 支持",
          "桌面应用每小时检查更新，设置中新增手动检查按钮",
        ],
        fixes: [
          "网页已登录时将会话交接给桌面应用",
          "修复 `?next=` 开放重定向漏洞",
          "OpenClaw 停止传递不支持的参数，正确传递 AgentInstructions",
        ],
      },
      {
        version: "0.2.5",
        date: "2026-04-17",
        title: "CLI Autopilot、Cmd+K 与 Daemon 身份",
        changes: [],
        features: [
          "CLI `autopilot` 命令，管理定时和触发式自动化",
          "CLI `issue subscriber` 订阅管理命令",
          "Cmd+K 命令面板扩展——主题切换、快速创建 Issue/项目、复制链接、切换工作区",
          "Issue 列表卡片可选显示项目和子 Issue 进度",
          "Daemon 持久化 UUID 身份——CLI 和桌面应用共用同一个 daemon，跨重启和机器迁移保持一致",
          "唯一所有者退出工作区的前置检查",
          "评论折叠状态跨会话持久化",
        ],
        fixes: [
          "Agent 现在在任意 Issue 状态下都会响应评论触发",
          "修复 Codex 沙箱在 macOS 上的网络访问配置",
          "编辑器气泡菜单改用 @floating-ui/dom 重写，滚动时正确隐藏",
          "Autopilot 创建者自动订阅其生成的 Issue",
          "Autopilot run-only 任务正确解析工作区 ID",
          "桌面应用 `shell.openExternal` 限制仅允许 http/https 协议（安全）",
          "重名 Agent 创建返回 409 而非静默失败",
          "桌面应用新建标签页继承当前工作区",
        ],
      },
      {
        version: "0.2.1",
        date: "2026-04-16",
        title: "新增 Agent 运行时",
        changes: [],
        features: [
          "支持 GitHub Copilot CLI 运行时",
          "支持 Cursor Agent CLI 运行时",
          "支持 Pi Agent 运行时",
          "工作区 URL 改造——slug 优先路由（`/{slug}/issues`），旧链接自动重定向",
        ],
        fixes: [
          "Codex 同一 Issue 下跨任务恢复会话线程",
          "Codex 回合错误正确抛出，不再报告空输出",
          "工作区用量按任务完成时间正确分桶",
          "Autopilot 运行历史行整行可点击",
          "Daemon 和 GC 端点加强工作区隔离校验（安全）",
          "邀请邮件中的工作区和邀请人名称进行 HTML 转义",
          "桌面应用开发版和生产版现在可以同时运行",
        ],
      },
      {
        version: "0.2.0",
        date: "2026-04-15",
        title: "桌面应用、Autopilot 与邀请",
        changes: [],
        features: [
          "macOS 桌面应用——原生 Electron 应用，支持标签页系统、内置 Daemon 管理、沉浸模式和自动更新",
          "Autopilot——Agent 定时和触发式自动化任务",
          "工作区邀请，支持邮件通知和专用接受页面",
          "Agent 自定义 CLI 参数，支持高级运行时配置",
          "聊天界面重设计，新增未读追踪和会话管理优化",
          "创建 Agent 对话框显示运行时所有者和 Mine/All 筛选",
        ],
        improvements: [
          "Inter 字体 + CJK 回退，中英文自动间距",
          "侧边栏用户菜单改为整行弹出面板",
          "WebSocket ping/pong 心跳检测断线连接",
          "普通成员现在可以创建 Agent 和管理自己的 Skills",
        ],
        fixes: [
          "Agent 在已参与的线程收到回复时正确触发",
          "自部署：Docker 本地上传文件持久化，WebSocket URL 自动适配局域网",
          "Cmd+K 最近 Issue 列表状态过期",
        ],
      },
      {
        version: "0.1.33",
        date: "2026-04-14",
        title: "Gemini CLI 与 Agent 环境变量",
        changes: [],
        features: [
          "Google Gemini CLI 作为新的 Agent 运行时，支持实时日志流",
          "Agent 自定义环境变量（router/proxy 模式），新增专用设置标签页",
          "Issue 右键菜单新增「设置父 Issue」和「添加子 Issue」",
          "CLI `--parent` 更新父 Issue，`--content-stdin` 管道输入评论内容",
          "子 Issue 自动继承父级项目",
        ],
        improvements: [
          "编辑器气泡菜单和链接预览重写",
          "OpenClaw 后端 P0+P1 优化（多行 JSON、增量解析）",
          "自部署 WebSocket URL 自动适配局域网访问",
        ],
        fixes: [
          "S3 上传路径按工作区隔离（安全）",
          "订阅和上传新增工作区成员身份校验（安全）",
          "Issue 状态改为已取消时自动终止进行中的任务",
          "Agent 进程 stdout 挂起导致任务卡住",
          "Daemon 触发提示现在嵌入实际的触发评论内容",
          "登录和仪表盘跳转稳定性改进",
        ],
      },
      {
        version: "0.1.28",
        date: "2026-04-13",
        title: "Windows 支持、认证与引导",
        changes: [],
        features: [
          "Windows 支持——CLI 安装、Daemon 运行和发布构建",
          "认证迁移至 HttpOnly Cookie，WebSocket 新增 Origin 白名单",
          "新工作区全屏引导向导",
          "Master Agent 聊天窗口可调整大小，会话历史体验优化",
          "OpenCode、OpenClaw 和 Hermes 运行时 Token 用量日志扫描",
        ],
        fixes: [
          "WebSocket 首条消息认证安全修复",
          "新增 Content-Security-Policy 响应头",
          "子 Issue 进度改为从数据库计算而非分页客户端缓存",
        ],
      },
      {
        version: "0.1.27",
        date: "2026-04-12",
        title: "一键安装、自部署与稳定性",
        changes: [],
        features: [
          "一键安装与配置——`curl | bash` 安装 CLI，`--with-server` 完整自部署，`multica setup` 配置连接环境",
          "自部署存储——无 S3 时本地文件存储回退，支持自定义 S3 端点（MinIO）",
          "项目列表页支持行内编辑属性（优先级、状态、负责人）",
        ],
        improvements: [
          "过期 Agent 任务自动清扫；执行卡片立即显示，无需等待首条消息",
          "通过 CLI 上传的评论附件现在可在 UI 中显示",
          "置顶项按用户隔离，修复侧边栏置顶操作",
        ],
        fixes: [
          "Daemon API 路由和附件上传新增工作区所有权校验",
          "Markdown 清洗器保留代码块不被 HTML 实体转义",
          "Next.js 升级至 ^16.2.3 修复 CVE-2026-23869",
          "OpenClaw 后端重写以匹配实际 CLI 接口",
        ],
      },
      {
        version: "0.1.24",
        date: "2026-04-11",
        title: "安全加固与通知",
        changes: [],
        features: [
          "子 Issue 变更时通知父 Issue 的订阅者",
          "CLI `--project` 筛选 Issue 列表",
        ],
        improvements: [
          "Meta-skill 工作流改为委托 Agent Skills 而非硬编码逻辑",
        ],
        fixes: [
          "Daemon API 路由新增工作区所有权校验",
          "附件上传和查询新增工作区所有权验证",
          "回复评论不再继承父级线程的 Agent 提及",
          "Agent 创建评论缺少 workspace ID",
          "自部署 Docker 构建问题修复（文件权限、CRLF 换行、缺失依赖）",
        ],
      },
      {
        version: "0.1.23",
        date: "2026-04-11",
        title: "置顶、Cmd+K 与项目增强",
        changes: [],
        features: [
          "Issue 和项目置顶到侧边栏，支持拖拽排序",
          "Cmd+K 命令面板——最近访问的 Issue、页面导航、项目搜索",
          "项目详情侧边栏属性面板（替代原概览标签页）",
          "Issues 列表新增项目筛选",
          "项目列表显示完成进度",
          "在项目页按 'C' 创建 Issue 时自动填充项目",
          "指派人下拉按用户分配频率排序",
        ],
        fixes: [
          "Markdown XSS 漏洞——评论渲染增加 rehype-sanitize 和服务端 bluemonday 清洗",
          "项目看板 Issue 计数不正确",
          "自部署 Docker 构建缺少 tsconfig 依赖",
          "Cmd+K 需要按两次 ESC 才能关闭",
        ],
      },
      {
        version: "0.1.22",
        date: "2026-04-10",
        title: "自部署、ACP 与文档站",
        changes: [],
        features: [
          "全栈 Docker Compose 一键自部署",
          "通过 ACP 协议接入 Hermes Agent Provider",
          "基于 Fumadocs 搭建文档站（快速入门、CLI 参考、Agent 指南）",
          "侧边栏和收件箱移动端响应式布局",
          "Issue 详情侧边栏展示 Token 用量",
          "支持在 UI 中切换 Agent 运行时",
          "'C' 快捷键快速创建 Issue",
          "聊天会话历史面板，查看已归档对话",
          "Daemon 新增 Claude Code 和 Codex 最低版本检查",
          "官网新增 OpenClaw 和 OpenCode 展示",
          "`make dev` 一键本地开发环境搭建",
        ],
        improvements: [
          "侧边栏重新设计——个人/工作区分组、用户档案底栏、⌘K 搜索入口",
          "搜索排序优化——大小写无关匹配、标识符搜索（MUL-123）、多词匹配",
          "搜索结果关键词高亮",
          "每日 Token 用量图表优化，Y 轴标签更清晰，新增分类 Tooltip",
          "Master Agent 支持多行输入",
          "统一选择器组件（状态、优先级、截止日期、项目、指派人）",
          "工作区级别存储隔离，切换工作区时自动加载对应数据",
          "自部署环境变量缺失时给出启动警告",
        ],
        fixes: [
          "删除子 Issue 后父级列表未刷新",
          "搜索索引兼容 RDS 上的 pg_bigm 1.2",
          "创建 Agent 对话框错误显示「无可用运行时」",
          "Claude stream-json 启动卡住",
          "多个 Agent 无法同时为同一 Issue 排队任务",
          "退出登录未清除工作区和查询缓存",
          "编辑器为空时拖放区域过小",
          "Skills 导入硬编码 main 分支导致 404",
          "WebSocket 端点不支持 PAT 认证",
          "所有 Agent 已归档时无法删除运行时",
        ],
      },
      {
        version: "0.1.21",
        date: "2026-04-09",
        title: "项目、搜索与 Monorepo",
        changes: [
          "项目实体全栈 CRUD——创建、编辑项目并按项目组织 Issue",
          "创建 Issue 弹窗新增项目选择器，CLI 新增项目命令",
          "基于 pg_bigm 的 Issue 全文搜索",
          "Monorepo 拆包——共享 core、UI、views 三个包（Turborepo）",
          "全屏 Agent 执行日志视图",
          "编辑器支持拖拽上传文件并展示文件卡片",
          "Issue 新增附件区域，支持图片网格和文件卡片展示",
          "运行时支持所有者追踪、筛选、头像展示和点对点更新通知",
          "列表视图行内显示子 Issue 进度",
          "列表视图支持已完成 Issue 分页加载",
          "Codex 会话日志扫描以报告 token 用量",
          "修复守护进程 repo 缓存卡在初始快照的问题",
        ],
      },
      {
        version: "0.1.20",
        date: "2026-04-08",
        title: "子 Issue、TanStack Query 与用量追踪",
        changes: [
          "子 Issue 支持——在任意 Issue 内创建、查看和管理子任务",
          "全面迁移至 TanStack Query 管理服务端状态（Issue、收件箱、工作区、运行时）",
          "按任务维度追踪所有 Agent 提供商的 token 用量",
          "同一 Issue 支持多个 Agent 并发执行",
          "看板视图：Done 列显示总数并支持无限滚动",
          "新增 ReadonlyContent 组件，轻量渲染评论中的 Markdown",
          "表情反应和变更操作支持乐观更新与回滚",
          "WebSocket 驱动缓存失效，替代轮询和焦点刷新",
          "CLI 登录流程中浏览器会话保持不丢失",
          "守护进程复用已有 worktree 时自动拉取最新远程代码",
          "修复动态根布局导致的标签页切换卡顿问题",
        ],
      },
      {
        version: "0.1.18",
        date: "2026-04-07",
        title: "OAuth、OpenClaw 与 Issue 加载优化",
        changes: [
          "支持 Google OAuth 登录",
          "新增 OpenClaw 运行时，支持在 OpenClaw 基础设施上运行 Agent",
          "Agent 实时卡片重新设计——始终吸顶，支持手动展开/收起",
          "打开的 Issue 不再分页限制全量加载，已关闭的 Issue 滚动分页",
          "JWT 和 CloudFront Cookie 有效期从 72 小时延长至 30 天",
          "重新登录后记住上次选择的工作区",
          "守护进程确保 Agent 任务环境中 multica CLI 在 PATH 上",
          "新增 PR 模板和面向 Agent 的 CLI 安装指南",
        ],
      },
      {
        version: "0.1.17",
        date: "2026-04-05",
        title: "评论分页与 CLI 优化",
        changes: [
          "评论列表支持分页，API 和 CLI 均已适配",
          "收件箱归档操作现在一次性归档同一 Issue 的所有通知",
          "CLI 帮助输出重新设计，匹配 gh CLI 风格并增加示例",
          "附件使用 UUIDv7 作为 S3 key，创建 Issue/评论时自动关联附件",
          "支持在已完成或已取消的 Issue 上 @提及已分配的 Agent",
          "回复仅 @提及成员时跳过父级提及继承逻辑",
          "Worktree 环境配置保留已有的 .env.worktree 变量",
        ],
      },
      {
        version: "0.1.15",
        date: "2026-04-03",
        title: "编辑器重构与 Agent 生命周期",
        changes: [
          "统一 Tiptap 编辑器，编辑和展示共用单一 Markdown 渲染管线",
          "Markdown 粘贴、行内代码间距和链接样式修复",
          "Agent 支持归档和恢复——软删除替代硬删除",
          "默认列表隐藏已归档的 Agent",
          "全应用新增骨架屏加载态、错误提示和确认对话框",
          "新增 OpenCode 作为支持的 Agent 提供商",
          "回复触发的 Agent 任务自动继承主线程 @提及",
          "Issue 和收件箱实时事件细粒度处理，不再全量刷新",
          "编辑器中统一图片上传流程，支持粘贴和按钮上传",
        ],
      },
      {
        version: "0.1.14",
        date: "2026-04-02",
        title: "提及与权限",
        changes: [
          "评论中支持 @提及 Issue，服务端自动展开",
          "支持 @all 提及工作区所有成员",
          "收件箱通知点击后自动滚动到对应评论",
          "仓库管理独立为设置页单独标签页",
          "支持从网页端运行时页面更新 CLI，非 Homebrew 安装支持直接下载更新",
          "新增 CLI 命令查看 Issue 执行记录和运行消息",
          "Agent 权限模型优化——所有者和管理员管理 Agent，成员可管理自己 Agent 的技能",
          "每个 Issue 串行执行，防止并发任务冲突",
          "文件上传支持所有文件类型",
          "README 重新设计，新增快速入门指南",
        ],
      },
      {
        version: "0.1.13",
        date: "2026-04-01",
        title: "\u6211\u7684 Issue \u4e0e\u56fd\u9645\u5316",
        changes: [
          "\u6211\u7684 Issue \u9875\u9762\uff0c\u652f\u6301\u770b\u677f\u3001\u5217\u8868\u89c6\u56fe\u548c\u8303\u56f4\u6807\u7b7e",
          "\u843d\u5730\u9875\u65b0\u589e\u7b80\u4f53\u4e2d\u6587\u672c\u5730\u5316",
          "\u65b0\u589e\u5173\u4e8e\u9875\u9762\u548c\u66f4\u65b0\u65e5\u5fd7\u9875\u9762",
          "Agent \u8bbe\u7f6e\u9875\u652f\u6301\u5934\u50cf\u4e0a\u4f20",
          "CLI \u8bc4\u8bba\u548c Issue/\u8bc4\u8bba API \u7684\u9644\u4ef6\u652f\u6301",
          "\u7edf\u4e00\u5934\u50cf\u6e32\u67d3\uff0c\u6240\u6709\u9009\u62e9\u5668\u4f7f\u7528 ActorAvatar \u7ec4\u4ef6",
          "\u843d\u5730\u9875 SEO \u4f18\u5316\u548c\u767b\u5f55\u6d41\u7a0b\u6539\u8fdb",
          "CLI \u9ed8\u8ba4\u4f7f\u7528\u751f\u4ea7\u73af\u5883 API \u5730\u5740",
          "\u8bb8\u53ef\u8bc1\u53d8\u66f4\u4e3a Apache 2.0",
        ],
      },
      {
        version: "0.1.3",
        date: "2026-03-31",
        title: "Agent \u667a\u80fd",
        changes: [
          "\u901a\u8fc7\u8bc4\u8bba\u4e2d\u7684 @\u63d0\u53ca\u89e6\u53d1 Agent",
          "\u5c06 Agent \u5b9e\u65f6\u8f93\u51fa\u63a8\u9001\u5230 Issue \u8be6\u60c5\u9875",
          "\u5bcc\u6587\u672c\u7f16\u8f91\u5668\u2014\u2014\u63d0\u53ca\u3001\u94fe\u63a5\u7c98\u8d34\u3001\u8868\u60c5\u53cd\u5e94\u3001\u53ef\u6298\u53e0\u7ebf\u7a0b",
          "\u6587\u4ef6\u4e0a\u4f20\uff0c\u652f\u6301 S3 + CloudFront \u7b7e\u540d URL \u548c\u9644\u4ef6\u8ddf\u8e2a",
          "Agent \u9a71\u52a8\u7684\u4ee3\u7801\u4ed3\u5e93\u68c0\u51fa\uff0c\u5e26 bare clone \u7f13\u5b58\u7684\u4efb\u52a1\u9694\u79bb",
          "Issue \u5217\u8868\u89c6\u56fe\u7684\u6279\u91cf\u64cd\u4f5c",
          "\u5b88\u62a4\u8fdb\u7a0b\u8eab\u4efd\u8ba4\u8bc1\u548c\u5b89\u5168\u52a0\u56fa",
        ],
      },
      {
        version: "0.1.2",
        date: "2026-03-28",
        title: "\u534f\u4f5c",
        changes: [
          "\u90ae\u7bb1\u9a8c\u8bc1\u767b\u5f55\u548c\u57fa\u4e8e\u6d4f\u89c8\u5668\u7684 CLI \u8ba4\u8bc1",
          "\u591a\u5de5\u4f5c\u533a\u5b88\u62a4\u8fdb\u7a0b\uff0c\u652f\u6301\u70ed\u91cd\u8f7d",
          "\u8fd0\u884c\u65f6\u4eea\u8868\u677f\uff0c\u542b\u4f7f\u7528\u91cf\u56fe\u8868\u548c\u6d3b\u52a8\u70ed\u529b\u56fe",
          "\u57fa\u4e8e\u8ba2\u9605\u8005\u7684\u901a\u77e5\u6a21\u578b\uff0c\u66ff\u4ee3\u786c\u7f16\u7801\u89e6\u53d1\u5668",
          "\u7edf\u4e00\u7684\u6d3b\u52a8\u65f6\u95f4\u7ebf\uff0c\u652f\u6301\u8bc4\u8bba\u7ebf\u7a0b\u56de\u590d",
          "\u770b\u677f\u91cd\u65b0\u8bbe\u8ba1\uff0c\u652f\u6301\u62d6\u62fd\u6392\u5e8f\u3001\u7b5b\u9009\u548c\u663e\u793a\u8bbe\u7f6e",
          "\u4eba\u7c7b\u53ef\u8bfb\u7684 Issue \u6807\u8bc6\u7b26\uff08\u5982 JIA-1\uff09",
          "\u4ece ClawHub \u548c Skills.sh \u5bfc\u5165\u6280\u80fd",
        ],
      },
      {
        version: "0.1.1",
        date: "2026-03-25",
        title: "\u6838\u5fc3\u5e73\u53f0",
        changes: [
          "\u591a\u5de5\u4f5c\u533a\u5207\u6362\u548c\u521b\u5efa",
          "Agent \u7ba1\u7406 UI\uff0c\u652f\u6301\u6280\u80fd\u3001\u5de5\u5177\u548c\u89e6\u53d1\u5668",
          "\u7edf\u4e00\u7684 Agent SDK\uff0c\u652f\u6301 Claude Code \u548c Codex \u540e\u7aef",
          "\u8bc4\u8bba CRUD\uff0c\u652f\u6301\u5b9e\u65f6 WebSocket \u66f4\u65b0",
          "\u4efb\u52a1\u670d\u52a1\u5c42\u548c\u5b88\u62a4\u8fdb\u7a0b REST \u534f\u8bae",
          "\u4e8b\u4ef6\u603b\u7ebf\uff0c\u652f\u6301\u5de5\u4f5c\u533a\u7ea7\u522b\u7684 WebSocket \u9694\u79bb",
          "\u6536\u4ef6\u7bb1\u901a\u77e5\uff0c\u652f\u6301\u672a\u8bfb\u5fbd\u7ae0\u548c\u5f52\u6863",
          "CLI \u652f\u6301 cobra \u5b50\u547d\u4ee4\uff0c\u7528\u4e8e\u5de5\u4f5c\u533a\u548c Issue \u7ba1\u7406",
        ],
      },
      {
        version: "0.1.0",
        date: "2026-03-22",
        title: "\u57fa\u7840\u67b6\u6784",
        changes: [
          "Go \u540e\u7aef\uff0c\u652f\u6301 REST API\u3001JWT \u8ba4\u8bc1\u548c\u5b9e\u65f6 WebSocket",
          "Next.js \u524d\u7aef\uff0cLinear \u98ce\u683c UI",
          "Issue \u652f\u6301\u770b\u677f\u548c\u5217\u8868\u89c6\u56fe\uff0c\u542b\u62d6\u62fd\u770b\u677f",
          "Agent\u3001\u6536\u4ef6\u7bb1\u548c\u8bbe\u7f6e\u9875\u9762",
          "\u4e00\u952e\u8bbe\u7f6e\u3001\u8fc1\u79fb CLI \u548c\u79cd\u5b50\u5de5\u5177",
          "\u5168\u9762\u6d4b\u8bd5\u5957\u4ef6\u2014\u2014Go \u5355\u5143/\u96c6\u6210\u6d4b\u8bd5\u3001Vitest\u3001Playwright E2E",
        ],
      },
    ],
  },
  download: {
    hero: {
      macArm64: {
        title: "Multica for macOS",
        sub: "Apple Silicon · 内置 daemon，无需配置",
        primary: "下载 (.dmg)",
        altZip: "或下载 .zip",
      },
      macIntel: {
        title: "Multica for macOS",
        sub: "需要 Apple Silicon——暂不支持 Intel Mac。",
        disabledCta: "需要 Apple Silicon",
        intelHint: "在 Intel Mac 上？请使用下方 CLI——底层跑的是同一个 daemon。",
      },
      winX64: {
        title: "Multica for Windows",
        sub: "内置 daemon，无需配置",
        primary: "下载 (.exe)",
      },
      winArm64: {
        title: "Multica for Windows",
        sub: "ARM · 内置 daemon，无需配置",
        primary: "下载 (.exe)",
      },
      linux: {
        title: "Multica for Linux",
        sub: "内置 daemon，无需配置",
        primary: "下载 AppImage",
        altFormats: "或 .deb / .rpm",
      },
      unknown: {
        title: "选择你的平台",
        sub: "下方是所有支持的安装包。",
      },
      safariMacHint: "在 Intel Mac 上？请使用下方 CLI。",
      archFallbackHint: "架构不对？下方是所有可选格式。",
    },
    allPlatforms: {
      title: "所有平台",
      macLabel: "macOS · Apple Silicon",
      winX64Label: "Windows · x64",
      winArm64Label: "Windows · ARM64",
      linuxX64Label: "Linux · x64",
      linuxArm64Label: "Linux · ARM64",
      formatDmg: ".dmg",
      formatZip: ".zip",
      formatExe: ".exe",
      formatAppImage: ".AppImage",
      formatDeb: ".deb",
      formatRpm: ".rpm",
      intelNote: "仅支持 Apple Silicon——Intel Mac 目前暂不支持。",
      unavailable: "暂不可用",
    },
    cli: {
      title: "想用 CLI？",
      sub: "适合服务器、远程开发机、无图形界面环境。底层 daemon 与 Desktop 相同，通过终端安装。",
      installLabel: "安装",
      startLabel: "启动 daemon",
      sshNote: "已经在服务器上？通过 SSH 执行同样的命令即可。",
      copyLabel: "复制",
      copiedLabel: "已复制",
    },
    cloud: {
      title: "Cloud runtime（等待名单）",
      sub: "我们将为你托管 runtime，目前尚未上线——留下邮箱，上线后通知你。",
    },
    footer: {
      releaseNotes: "v{version} 更新内容",
      allReleases: "查看所有版本",
      currentVersion: "当前版本：{version}",
      versionUnavailable: "版本获取失败——请前往 GitHub 查看",
    },
  },
  };
}
