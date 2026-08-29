// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  desktopTitle,
  type DesktopTitleKey,
  type LayoutTranslator,
} from "./desktop-title";

const translations = {
  nav: {
    issues: "任务",
    projects: "项目",
    my_issues: "我的任务",
    runtimes: "运行时",
    skills: "Skills",
    agents: "智能体",
    squads: "小队",
    inbox: "收件箱",
    chat: "聊天",
    usage: "统计",
    settings: "设置",
  },
  tab: {
    issue: "任务",
    project: "项目",
    autopilot: "自动化",
    machine: "机器",
    runtime: "运行时",
    skill: "Skill",
    create_agent: "创建智能体",
    agent: "智能体",
    member: "成员",
    squad: "小队",
    attachment: "附件",
  },
};

const t = ((selector: (resources: typeof translations) => string) =>
  selector(translations)) as LayoutTranslator;

const cases: Array<[DesktopTitleKey, string]> = [
  ["issues", "任务"],
  ["issue", "任务"],
  ["projects", "项目"],
  ["project", "项目"],
  ["autopilot", "自动化"],
  ["my_issues", "我的任务"],
  ["runtimes", "运行时"],
  ["machine", "机器"],
  ["runtime", "运行时"],
  ["skills", "Skills"],
  ["skill", "Skill"],
  ["agents", "智能体"],
  ["create_agent", "创建智能体"],
  ["agent", "智能体"],
  ["member", "成员"],
  ["squads", "小队"],
  ["squad", "小队"],
  ["inbox", "收件箱"],
  ["chat", "聊天"],
  ["attachment", "附件"],
  ["usage", "统计"],
  ["settings", "设置"],
];

describe("desktopTitle", () => {
  it.each(cases)("resolves %s through the selected locale", (key, expected) => {
    expect(desktopTitle(key, t)).toBe(expected);
  });
});
