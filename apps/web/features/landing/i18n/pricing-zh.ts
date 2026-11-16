import type { PricingDict } from "./types";

export const zhPricingCopy: PricingDict = {
  eyebrow: "定价",
  title: "按团队成员付费，智能体随便用。",
  lead: "每个套餐都可以在自己的运行时上使用不限数量的智能体，你自己发起的运行从不计数。Claude Code、Codex 或任何你已经在用的编码智能体都可以接入。",
  launchOffer: { badge: "发布优惠", text: "{date}前订阅年付，首年 7 折" },
  billing: { label: "付费周期", yearly: "年付", monthly: "月付", save: "省 20%" },
  recommended: "推荐",
  soon: "即将推出",
  included: "包含",
  notIncluded: "不包含",
  yearlyNote: "按月付费 {price}",
  monthlyNote: "按年付费每月 {price}",
  firstYear: "首年 {price}",
  plans: {
    free: {
      name: "Free",
      audience: "适合个人和刚起步的小团队。",
      cadence: "永久免费",
      cta: "免费开始",
      ctaNote: "不需要绑卡",
      includes: "包含",
      features: [
        { label: "最多 5 位成员" },
        { label: "智能体、运行时、小队和技能不限数量" },
        { label: "任务、项目和运行记录不限" },
        { label: "每月 200 次 Autopilot 运行" },
        { label: "1 GB 文件存储" },
        { label: "统计看板最近 30 天" },
      ],
    },
    pro: {
      name: "Pro",
      audience: "去掉限制，适合每天都在交付的团队。",
      cadence: "每人每月",
      cta: "开始使用",
      ctaNote: "随时可以取消",
      includes: "Free 的全部功能，以及",
      features: [
        { label: "成员不限" },
        { label: "免费访客，不占席位" },
        { label: "每人每月 1,000 次 Autopilot 运行" },
        { label: "每人 10 GB 存储" },
        { label: "统计看板最近 90 天" },
        { label: "Multica Cloud 额度", soon: true },
      ],
    },
    business: {
      name: "Business",
      audience: "团队变大后，管好谁能看、谁能用。",
      cadence: "每人每月",
      cta: "免费试用 14 天",
      ctaNote: "不需要绑卡 ·",
      ctaNoteLink: "联系销售",
      includes: "Pro 的全部功能，以及",
      features: [
        { label: "私有项目" },
        { label: "按成员或团队分配智能体和运行时" },
        { label: "团队应用账号和审批规则" },
        { label: "强制 Google 登录和域名自动加入" },
        { label: "按成员的用量和成本报表" },
        { label: "每人每月 3,000 次 Autopilot 运行" },
        { label: "每人 100 GB 存储" },
        { label: "统计看板最近 365 天" },
      ],
    },
    enterprise: {
      name: "Enterprise",
      audience: "满足安全、合规和私有化部署的需求。",
      cadence: "按年签约",
      cta: "联系销售",
      ctaNote: "Multica Cloud 或私有化部署",
      includes: "Business 的全部功能，以及",
      features: [
        { label: "SAML 单点登录和 SCIM" },
        { label: "审计日志" },
        { label: "数据保留和删除策略" },
        { label: "IP 白名单" },
        { label: "自定义品牌和域名" },
        { label: "私有化部署" },
        { label: "对公转账和专属支持" },
      ],
    },
  },
  customPrice: "定制",
  cloudCredits: {
    title: "Multica Cloud 额度",
    body: "不用自己的电脑，也能在托管的运行时和模型上跑智能体。额度按接近成本的价格计费，在工作区内共享，自托管的实例以后也能购买。",
  },
  selfHost: {
    title: "想自己部署 Multica？",
    body: "社区版在你的组织内部使用是免费的。Enterprise 额外提供单点登录、审计日志、自定义品牌和私有化部署支持。",
    contactSales: "联系销售",
    licensing: "许可协议",
  },
  compare: {
    title: "套餐对比",
    lead: "所有套餐的智能体都不限数量。Pro 去掉限制，Business 增加权限和管理。",
    groups: [
      {
        title: "团队",
        rows: [
          { label: "成员", cells: ["最多 5 位", "不限", "不限", "不限"] },
          { label: "访客", hint: "可查看和评论，免费，不占席位", cells: [false, true, true, true] },
          { label: "任务、项目和运行记录", cells: ["不限", "不限", "不限", "不限"] },
          { label: "文件存储", cells: ["1 GB", "每人 10 GB", "每人 100 GB", "定制"] },
        ],
      },
      {
        title: "智能体和自动化",
        rows: [
          { label: "智能体、运行时、小队和技能", cells: ["不限", "不限", "不限", "不限"] },
          { label: "你自己发起的运行", cells: ["不限", "不限", "不限", "不限"] },
          {
            label: "Autopilot 运行",
            hint: "在工作区内共享",
            cells: ["每月 200 次", "每人每月 1,000 次", "每人每月 3,000 次", "定制"],
          },
          { label: "加购 Autopilot 运行", cells: [false, "$10 / 1,000 次", "$10 / 1,000 次", "定制"] },
          { label: "Multica Cloud 额度", soon: true, cells: [false, "包含", "包含更多", "定制"] },
        ],
      },
      {
        title: "统计",
        rows: [
          { label: "统计看板查看范围", cells: ["30 天", "90 天", "365 天", "定制"] },
          { label: "按成员的用量和成本，导出 CSV", cells: [false, false, true, true] },
        ],
      },
      {
        title: "权限和管理",
        rows: [
          { label: "私有项目", cells: [false, false, true, true] },
          { label: "指定成员或团队使用智能体", cells: [false, false, true, true] },
          { label: "运行时共享给指定成员", cells: [false, false, true, true] },
          { label: "团队应用账号和审批规则", cells: [false, false, true, true] },
        ],
      },
      {
        title: "安全和管理员",
        rows: [
          { label: "管理员角色", cells: [true, true, true, true] },
          { label: "强制 Google 登录", cells: [false, false, true, true] },
          { label: "域名自动加入", cells: [false, false, true, true] },
          { label: "SAML 单点登录和 SCIM", cells: [false, false, false, true] },
          { label: "审计日志", cells: [false, false, false, true] },
          { label: "数据保留、删除和 IP 白名单", cells: [false, false, false, true] },
          { label: "自定义品牌和域名", cells: [false, false, false, true] },
          { label: "私有化部署", cells: [false, false, false, true] },
        ],
      },
      {
        title: "付款和支持",
        rows: [
          { label: "付款方式", cells: ["—", "银行卡", "银行卡", "对公转账和采购单"] },
          { label: "支持", cells: ["社区", "邮件", "优先邮件", "专属支持"] },
        ],
      },
    ],
  },
  faq: {
    title: "常见问题",
    items: [
      {
        question: "智能体算成员吗？",
        answer: "不算。你只为团队里的人付费。所有套餐的智能体、运行时、小队和技能都不限数量。",
      },
      {
        question: "什么算一次 Autopilot 运行？",
        answer:
          "Autopilot 按计划或触发条件发起的每一次运行。你自己发起的运行，比如分派任务、聊天或 @ 智能体，从不计数。运行次数在工作区内共享，Pro 和 Business 可以按 $10 / 1,000 次加购。",
      },
      {
        question: "访客是谁？",
        answer: "只需要跟进进度的人。访客可以查看和评论分享给他们的项目，不占席位，在 Pro 和 Business 上免费。",
      },
      {
        question: "试用结束后会怎样？",
        answer:
          "新工作区可以免费试用 14 天 Business，不需要绑卡。试用结束后，你可以选择一个套餐，或者切换到 Free。数据不会被删除，已经设置好的 Business 功能继续生效，升级后可以再修改。",
      },
      {
        question: "发布优惠怎么用？",
        answer: "在{date}前订阅 Pro 或 Business 年付，首年 7 折。续费按正常的年付价格。",
      },
      {
        question: "可以自己部署 Multica 吗？",
        answer:
          "可以。社区版在你的组织内部使用是免费的。Enterprise 额外提供单点登录、审计日志、自定义品牌，以及私有化部署的支持。",
      },
    ],
  },
};
