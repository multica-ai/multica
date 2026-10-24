import { useState } from "react";
import { toast } from "sonner";
import { AgentPage } from "./agent-page";
import {
  contentHash,
  initialAgents,
  initialRevisions,
  runs as initialRuns,
  SKILL_NAME,
  type Binding,
  type MockRun,
  type SkillFiles,
  type SkillRevision,
} from "./fixtures";
import { RunPage } from "./run-page";
import { SkillPage, type SaveResult, type SkillPageState } from "./skill-page";

// Design mock for MUL-8089 (Skill versioning MVP). Three production surfaces —
// the run record on an issue, the skill detail page and the agent's Skills tab
// — share one in-memory store, so a rollback or a pin made on one page shows
// up on the others exactly as the proposed API would make it.

const RUN_PATH = "/ui-lab/issues/MUL-8120/runs";
const SKILL_PATH = "/ui-lab/skills/code-review";
const AGENT_PATH = "/ui-lab/agents";

export const SKILL_VERSIONS_START = RUN_PATH;

export function isSkillVersionsPath(path: string): boolean {
  return (
    path === "/ui-lab/issues" ||
    path.startsWith("/ui-lab/issues/") ||
    path.startsWith("/ui-lab/skills") ||
    path.startsWith(AGENT_PATH)
  );
}

export function SkillVersionsScene({
  path,
  navigate,
}: {
  path: string;
  navigate: (path: string) => void;
}) {
  const [revisions, setRevisions] = useState<SkillRevision[]>(initialRevisions);
  const [agents, setAgents] = useState(initialAgents);
  const [runs, setRuns] = useState<MockRun[]>(initialRuns);
  const [agentId, setAgentId] = useState("agent-release");
  const [skillView, setSkillView] = useState<SkillPageState>({
    tab: "history",
    selected: 7,
    from: 6,
    to: 7,
  });
  // The first save simulates a teammate committing in between, so the
  // optimistic-lock banner can be reviewed; later saves go straight through.
  const [conflictShown, setConflictShown] = useState(false);
  const current = revisions[revisions.length - 1]!;

  const commit = (
    files: SkillFiles,
    fields: Pick<SkillRevision, "source" | "restoredFrom" | "author" | "note">,
  ): SkillRevision | null => {
    // Same content hash as the current revision → no new revision.
    if (contentHash(files) === contentHash(current.files)) return null;
    const next: SkillRevision = {
      id: `rev-${current.number + 1}`,
      number: current.number + 1,
      createdAt: "刚刚",
      files,
      ...fields,
    };
    setRevisions((prev) => [...prev, next]);
    return next;
  };

  const openSkill = (view: Partial<SkillPageState>) => {
    setSkillView((prev) => ({ ...prev, ...view }));
    navigate(SKILL_PATH);
  };

  const restore = (n: number, note: string) => {
    const source = revisions.find((revision) => revision.number === n)!;
    const next = commit(source.files, {
      source: "restore",
      restoredFrom: n,
      author: { type: "member", name: "Bohan" },
      note,
    });
    if (!next) {
      toast.info("内容和当前版本相同，没有生成新版本");
      return;
    }
    setSkillView({ tab: "history", selected: next.number, from: current.number, to: next.number });
    toast.success(`已回滚，当前版本 #${next.number}`, {
      description: "跟随最新的 agent 从下一个任务开始使用",
    });
  };

  const save = (content: string, note: string, overwrite: boolean): SaveResult => {
    if (!overwrite && !conflictShown) {
      setConflictShown(true);
      const theirs = commit(
        {
          ...current.files,
          "refs/style.md": `${current.files["refs/style.md"]}\n- 公共函数必须写注释`,
        },
        {
          source: "edit",
          restoredFrom: null,
          author: { type: "member", name: "Jiayuan" },
          note: "风格约定：公共函数写注释",
        },
      );
      return { status: "conflict", number: theirs?.number ?? current.number };
    }
    const next = commit(
      { ...current.files, "SKILL.md": content },
      {
        source: "edit",
        restoredFrom: null,
        author: { type: "member", name: "Bohan" },
        note,
      },
    );
    if (!next) {
      toast.info("内容没有变化，没有生成新版本");
      return { status: "unchanged", number: current.number };
    }
    toast.success(`已保存为 #${next.number}`);
    return { status: "saved", number: next.number };
  };

  const bind = (id: string, binding: Binding) => {
    setAgents((prev) => prev.map((agent) => (agent.id === id ? { ...agent, binding } : agent)));
    const name = agents.find((agent) => agent.id === id)?.name;
    toast.success(
      binding === null
        ? `${name} 改为跟随最新（#${current.number}）`
        : `${name} 已固定在 #${binding}`,
    );
  };

  const run = (id: string) => {
    const agent = agents.find((item) => item.id === id)!;
    const revision = agent.binding ?? current.number;
    const files = revisions.find((item) => item.number === revision)!.files;
    const template = initialRuns[0]!;
    setRuns((prev) => [
      {
        ...template,
        id: `run-${1043 + prev.length}`,
        agentId: id,
        startedAt: "刚刚",
        duration: "3 分 40 秒",
        summary: `使用 ${SKILL_NAME} #${revision} 完成审查。`,
        skills: template.skills.map((use) =>
          use.kind === "workspace" && use.skill === SKILL_NAME
            ? {
                ...use,
                revision,
                binding: agent.binding === null ? "latest" : "pinned",
                hash: contentHash(files),
              }
            : use,
        ),
      },
      ...prev,
    ]);
  };

  if (path.startsWith("/ui-lab/skills")) {
    return (
      <SkillPage
        revisions={revisions}
        agents={agents}
        view={skillView}
        onView={(next) => setSkillView((prev) => ({ ...prev, ...next }))}
        onRestore={restore}
        onSave={save}
        navigate={navigate}
      />
    );
  }
  if (path.startsWith(AGENT_PATH)) {
    return (
      <AgentPage
        agent={agents.find((agent) => agent.id === agentId)!}
        agents={agents}
        revisions={revisions}
        onSelectAgent={setAgentId}
        onBind={bind}
        onCompare={(from, to) => openSkill({ tab: "history", from, to, selected: from })}
        navigate={navigate}
      />
    );
  }
  return (
    <RunPage
      runs={runs}
      agents={agents}
      revisions={revisions}
      onRun={run}
      onOpenRevision={(n) =>
        openSkill({ tab: "history", selected: n, to: n, from: Math.max(1, n - 1) })
      }
      onCompare={(from, to) => openSkill({ tab: "history", from, to, selected: from })}
      navigate={navigate}
    />
  );
}
