// Mock data for the Skill versioning MVP design review (MUL-8089).
//
// Nothing here talks to a server: the API this would come from
// (`GET /api/skills/{id}/revisions`, `.../compare`, `.../restore`,
// `PUT /api/agents/{id}/skills/{skillId}/pin`, `GET /api/tasks/{id}/skills`)
// is still a design. The shapes mirror the response fields proposed in the
// issue so the mock can be swapped for real queries without reshaping props.

export type ActorType = "member" | "agent" | "system" | "plugin";
export type RevisionSource =
  | "create"
  | "edit"
  | "import"
  | "refresh"
  | "plugin"
  | "restore"
  | "backfill";

export type SkillFiles = Record<string, string>;

export type SkillRevision = {
  id: string;
  number: number;
  source: RevisionSource;
  restoredFrom: number | null;
  author: { type: ActorType; name: string };
  note: string;
  createdAt: string;
  files: SkillFiles;
};

/** null = follow latest, a number = pinned to that revision. */
export type Binding = number | null;

export type MockAgent = {
  id: string;
  name: string;
  description: string;
  binding: Binding;
};

export type TaskSkillUse =
  | {
      kind: "workspace";
      skill: string;
      revision: number;
      binding: "latest" | "pinned";
      hash: string;
      deleted?: boolean;
    }
  | { kind: "builtin"; skill: string; serverVersion: string; hash: string };

export type MockRun = {
  id: string;
  agentId: string;
  issue: string;
  issueTitle: string;
  startedAt: string;
  duration: string;
  status: "completed" | "failed";
  summary: string;
  skills: TaskSkillUse[];
};

export const SKILL_NAME = "code-review";
export const SKILL_DESCRIPTION = "按团队约定审查 PR，输出可执行的修改建议";

const r1: SkillFiles = {
  "SKILL.md": [
    "---",
    "name: code-review",
    "description: 按团队约定审查 PR",
    "---",
    "",
    "# Code review",
    "",
    "审查 PR 时：",
    "1. 先读 PR 描述和关联 issue",
    "2. 按文件逐个检查",
    "3. Output as bullet list",
  ].join("\n"),
  "refs/style.md": ["# 风格约定", "", "- 命名用 camelCase", "- 函数不超过 80 行"].join(
    "\n",
  ),
};
const r2: SkillFiles = {
  ...r1,
  "SKILL.md": r1["SKILL.md"]!.replace(
    "2. 按文件逐个检查",
    "2. 按文件逐个检查\n3. 运行 scripts/lint.sh，把失败项列在最前面",
  ).replace("3. Output as bullet list", "4. Output as bullet list"),
  "scripts/lint.sh": ["#!/bin/sh", "set -e", "pnpm lint --quiet"].join("\n"),
};
const r3: SkillFiles = {
  ...r2,
  "SKILL.md": r2["SKILL.md"]!.replace(
    "4. Output as bullet list",
    "4. 检查是否泄露密钥或个人信息\n5. Output as bullet list",
  ),
};
const r4: SkillFiles = {
  ...r3,
  "refs/style.md": r3["refs/style.md"] + "\n- 禁止使用 any",
};
const r5: SkillFiles = {
  ...r4,
  "refs/security.md": [
    "# 安全检查清单",
    "",
    "- 不在日志里打印 token",
    "- SQL 一律参数化",
  ].join("\n"),
};
const r6: SkillFiles = {
  ...r5,
  "SKILL.md": r5["SKILL.md"]!.replace(
    "4. 检查是否泄露密钥或个人信息",
    "4. 按 refs/security.md 做安全检查",
  ),
};
const r7: SkillFiles = {
  ...r6,
  "SKILL.md": r6["SKILL.md"]!.replace(
    "5. Output as bullet list",
    "5. 用表格输出：文件 | 问题 | 建议 | 严重程度\n6. 没有问题的文件不要列出",
  ),
  "refs/style.md": r6["refs/style.md"]!.replace("80 行", "60 行"),
};

export const initialRevisions: SkillRevision[] = [
  {
    id: "rev-1",
    number: 1,
    source: "backfill",
    restoredFrom: null,
    author: { type: "member", name: "Bohan" },
    note: "",
    createdAt: "10-08 14:20",
    files: r1,
  },
  {
    id: "rev-2",
    number: 2,
    source: "edit",
    restoredFrom: null,
    author: { type: "member", name: "Bohan" },
    note: "加入 lint 脚本",
    createdAt: "10-11 10:05",
    files: r2,
  },
  {
    id: "rev-3",
    number: 3,
    source: "edit",
    restoredFrom: null,
    author: { type: "agent", name: "Eve" },
    note: "补充安全检查项",
    createdAt: "10-13 09:42",
    files: r3,
  },
  {
    id: "rev-4",
    number: 4,
    source: "edit",
    restoredFrom: null,
    author: { type: "member", name: "Jiayuan" },
    note: "风格约定：禁止 any",
    createdAt: "10-15 16:18",
    files: r4,
  },
  {
    id: "rev-5",
    number: 5,
    source: "refresh",
    restoredFrom: null,
    author: { type: "system", name: "system" },
    note: "从 GitHub 刷新",
    createdAt: "10-17 09:30",
    files: r5,
  },
  {
    id: "rev-6",
    number: 6,
    source: "edit",
    restoredFrom: null,
    author: { type: "agent", name: "Eve" },
    note: "安全检查改为引用清单",
    createdAt: "10-18 11:02",
    files: r6,
  },
  {
    id: "rev-7",
    number: 7,
    source: "edit",
    restoredFrom: null,
    author: { type: "member", name: "Bohan" },
    note: "输出改成表格",
    createdAt: "10-19 16:20",
    files: r7,
  },
];

export const initialAgents: MockAgent[] = [
  {
    id: "agent-reviewer",
    name: "reviewer-bot",
    description: "PR 审查",
    binding: null,
  },
  { id: "agent-qa", name: "qa-bot", description: "回归测试", binding: null },
  {
    id: "agent-release",
    name: "release-bot",
    description: "发布前检查",
    binding: 5,
  },
];

export const runs: MockRun[] = [
  {
    id: "run-1042",
    agentId: "agent-reviewer",
    issue: "MUL-8120",
    issueTitle: "审查 PR #9301：迁移 webhook 重试逻辑",
    startedAt: "10-19 17:05",
    duration: "4 分 12 秒",
    status: "completed",
    summary: "输出了一张 14 行的表格，但把没有问题的文件也列了出来。",
    skills: [
      {
        kind: "workspace",
        skill: SKILL_NAME,
        revision: 7,
        binding: "latest",
        hash: contentHash(r7),
      },
      {
        kind: "workspace",
        skill: "release-notes",
        revision: 3,
        binding: "pinned",
        hash: "sha256:91be44d20a6f",
      },
      {
        kind: "builtin",
        skill: "multica-platform",
        serverVersion: "v0.9.4",
        hash: "sha256:0d77e1c5b9a2",
      },
      {
        kind: "workspace",
        skill: "legacy-checklist",
        revision: 2,
        binding: "latest",
        hash: "sha256:5c10fa8e33d1",
        deleted: true,
      },
    ],
  },
];

/**
 * A short, stable stand-in for the server's bundle hash so the mock can show
 * that identical content means an identical hash (and therefore no new
 * revision).
 */
export function contentHash(files: SkillFiles): string {
  let h = 2166136261;
  const text = JSON.stringify(
    Object.keys(files)
      .sort()
      .map((path) => [path, files[path]]),
  );
  for (let i = 0; i < text.length; i++) {
    h ^= text.charCodeAt(i);
    h = Math.imul(h, 16777619) >>> 0;
  }
  return `sha256:${h.toString(16).padStart(8, "0")}`;
}
