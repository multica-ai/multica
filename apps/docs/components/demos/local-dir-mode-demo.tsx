"use client";

import { useState } from "react";
import { Check, Clock, FileText, Folder, GitBranch, Loader2, Pencil } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoAvatar,
  DemoFrame,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type Mode = "in_place" | "worktree";
type RunStatus = "idle" | "waiting" | "working" | "completed";

// Branch names follow server/internal/daemon/execenv/local_worktree.go:
// agent/<sanitized agent name>/<sanitized issue identifier>. The agent segment
// keeps only a-z and 0-9, so names without Latin letters become "agent".
const copy = {
  en: {
    title: "OPS-21 and OPS-22 · Two runs on one local folder",
    path: "~/Documents/website-copy",
    agent: "Ops helper",
    issues: [
      { id: "OPS-21", title: "Update the pricing page copy", file: "pricing.md", branch: "agent/ops-helper/ops-21" },
      { id: "OPS-22", title: "Add answers to the FAQ page", file: "faq.md", branch: "agent/ops-helper/ops-22" },
    ],
    dialogTitle: "How should runs use this folder?",
    modes: {
      in_place: {
        title: "Edit this folder directly",
        description:
          "One run at a time. Changes appear in your working copy as the agent makes them, ready for you to keep working on.",
      },
      worktree: {
        title: "Run in parallel, isolated",
        description:
          "Runs execute at the same time without touching your working copy. Each one hands back an agent/… branch in this repo for you to review and merge.",
      },
    } as Record<Mode, { title: string; description: string }>,
    add: "Add folder",
    badge: { in_place: "Direct", worktree: "Parallel" } as Record<Mode, string>,
    startSecond: "Start the second run",
    status: {
      working: "Working",
      waiting: "Waiting for local directory",
      completed: "Completed",
    } as Record<Exclude<RunStatus, "idle">, string>,
    isWaiting: (name: string) => `${name} is waiting for the local directory`,
    branch: "Branch",
    steps: (file: string) => ["Read the issue and discussion", `Edit ${file}`, "Write up what changed"],
    folderLabel: "Your folder",
    noChanges: "No changes",
    modified: "Modified",
    captions: {
      idle: "Two issues both need to edit the website copy in the same local folder.",
      choose:
        "The folder is a git repository, so Desktop preselects Parallel. To see Direct, pick it first; then click Add folder.",
      starting: "Ops helper starts working on OPS-21.",
      ready: "OPS-21 is still running. Click Start the second run so OPS-22 uses the same folder too.",
      waiting: "Direct runs one at a time: OPS-22 waits for the local directory and leaves the folder alone.",
      handoff: "OPS-21 finished and released the folder, so OPS-22 starts now.",
      parallel: "In Parallel mode both runs go at once, each in its own worktree. Your folder stays as it was.",
      doneInPlace: "The runs took turns, and their edits landed straight in your folder. Use Replay to try Parallel.",
      doneWorktree:
        "Each run handed back an agent/… branch and your folder is unchanged. Review the branches, then decide what to merge. Use Replay to try Direct.",
    },
  },
  zh: {
    title: "OPS-21 和 OPS-22 · 同一个本地文件夹上的两次运行",
    path: "~/Documents/website-copy",
    agent: "运营助手",
    issues: [
      { id: "OPS-21", title: "更新价格页文案", file: "pricing.md", branch: "agent/agent/ops-21" },
      { id: "OPS-22", title: "补充常见问题", file: "faq.md", branch: "agent/agent/ops-22" },
    ],
    dialogTitle: "每次运行如何使用这个文件夹？",
    modes: {
      in_place: {
        title: "直接修改这个文件夹",
        description: "同一时间只处理一次运行。智能体的改动会直接出现在你的工作区里，你可以接着改。",
      },
      worktree: {
        title: "并行隔离运行",
        description:
          "运行可以并行，互不干扰，也不会改动你的工作区。每次运行的结果都是这个仓库里的一个 agent/… 分支，由你 review 后合并。",
      },
    } as Record<Mode, { title: string; description: string }>,
    add: "添加文件夹",
    badge: { in_place: "原地", worktree: "并行" } as Record<Mode, string>,
    startSecond: "开始第二次运行",
    status: {
      working: "正在工作",
      waiting: "等待本地目录释放",
      completed: "已完成",
    } as Record<Exclude<RunStatus, "idle">, string>,
    isWaiting: (name: string) => `${name} 正在等待本地目录`,
    branch: "分支",
    steps: (file: string) => ["读取任务和讨论", `修改 ${file}`, "整理改动说明"],
    folderLabel: "你的文件夹",
    noChanges: "没有改动",
    modified: "已修改",
    captions: {
      idle: "两条任务都要修改同一个本地文件夹里的官网文案。",
      choose: `这个文件夹是 git 仓库，Desktop 预选了"并行"。想看"原地"就先点它，再点"添加文件夹"。`,
      starting: "运营助手开始处理 OPS-21。",
      ready: `OPS-21 还没做完。点"开始第二次运行"，让 OPS-22 也用这个文件夹。`,
      waiting: `"原地"同一时间只进行一次运行：OPS-22 进入"等待本地目录释放"，不会动文件夹。`,
      handoff: "OPS-21 结束并释放了文件夹，OPS-22 接着开始。",
      parallel: `"并行"模式下两次运行同时进行，各自在单独的 worktree 里工作，你的文件夹保持原样。`,
      doneInPlace: `两次运行轮流完成，改动直接写进了你的文件夹。点右上角可以换"并行"再试。`,
      doneWorktree: `每次运行交回一个 agent/… 分支，你的文件夹没有改动。看过分支后由你决定合并哪些。点右上角可以换"原地"再试。`,
    },
  },
  ja: {
    title: "OPS-21 と OPS-22 · 同じローカルフォルダでの 2 つの実行",
    path: "~/Documents/website-copy",
    agent: "運用アシスタント",
    issues: [
      { id: "OPS-21", title: "料金ページの文言を更新", file: "pricing.md", branch: "agent/agent/ops-21" },
      { id: "OPS-22", title: "よくある質問を追加", file: "faq.md", branch: "agent/agent/ops-22" },
    ],
    dialogTitle: "実行はこのフォルダをどう使いますか？",
    modes: {
      in_place: {
        title: "このフォルダを直接編集する",
        description: "実行は一度に1つずつ。エージェントの変更はそのまま作業コピーに現れ、続けて作業できます。",
      },
      worktree: {
        title: "隔離して並行実行する",
        description:
          "実行は、作業コピーに触れずに並行して進みます。それぞれがこのリポジトリの agent/… ブランチを返すので、レビューしてマージします。",
      },
    } as Record<Mode, { title: string; description: string }>,
    add: "フォルダを追加",
    badge: { in_place: "直接", worktree: "並行" } as Record<Mode, string>,
    startSecond: "2 回目の実行を開始",
    status: {
      working: "作業中",
      waiting: "ローカルディレクトリ待機中",
      completed: "完了",
    } as Record<Exclude<RunStatus, "idle">, string>,
    isWaiting: (name: string) => `${name} がローカルディレクトリを待機中`,
    branch: "ブランチ",
    steps: (file: string) => ["タスクの説明とコメントを読む", `${file} を編集`, "変更内容をまとめる"],
    folderLabel: "あなたのフォルダ",
    noChanges: "変更なし",
    modified: "変更あり",
    captions: {
      idle: "2 つのタスクが、同じローカルフォルダにある Web サイトの文言を編集します。",
      choose:
        "このフォルダは git リポジトリなので、Desktop は「並行」を選んだ状態で開きます。「直接」を見たい場合は先にそれを選び、「フォルダを追加」をクリックします。",
      starting: "運用アシスタントが OPS-21 の作業を始めます。",
      ready: "OPS-21 はまだ作業中です。「2 回目の実行を開始」をクリックして、OPS-22 にも同じフォルダを使わせます。",
      waiting: "「直接」では実行は一度に 1 つです。OPS-22 はローカルディレクトリ待ちになり、フォルダには触れません。",
      handoff: "OPS-21 が終わってフォルダを解放したので、OPS-22 が始まります。",
      parallel: "「並行」では 2 つの実行が同時に進み、それぞれ専用のワークツリーで作業します。フォルダはそのままです。",
      doneInPlace: "2 つの実行が順番に終わり、変更はフォルダに直接書き込まれました。「もう一度」で「並行」も試せます。",
      doneWorktree:
        "それぞれの実行が agent/… ブランチを返し、フォルダは変わっていません。ブランチを確認してから、何をマージするか決めます。「もう一度」で「直接」も試せます。",
    },
  },
  ko: {
    title: "OPS-21과 OPS-22 · 같은 로컬 폴더에서 두 번 실행",
    path: "~/Documents/website-copy",
    agent: "운영 도우미",
    issues: [
      { id: "OPS-21", title: "요금 페이지 문구 업데이트", file: "pricing.md", branch: "agent/agent/ops-21" },
      { id: "OPS-22", title: "자주 묻는 질문 보충", file: "faq.md", branch: "agent/agent/ops-22" },
    ],
    dialogTitle: "실행이 이 폴더를 어떻게 사용할까요?",
    modes: {
      in_place: {
        title: "이 폴더를 직접 수정",
        description:
          "한 번에 하나의 실행만 진행됩니다. 에이전트의 변경 사항이 작업 사본에 바로 나타나므로 이어서 작업할 수 있습니다.",
      },
      worktree: {
        title: "격리해서 병렬 실행",
        description:
          "실행이 작업 사본을 건드리지 않고 동시에 진행됩니다. 각 실행은 이 저장소의 agent/… 브랜치를 돌려주며, 검토 후 병합하면 됩니다.",
      },
    } as Record<Mode, { title: string; description: string }>,
    add: "폴더 추가",
    badge: { in_place: "직접", worktree: "병렬" } as Record<Mode, string>,
    startSecond: "두 번째 실행 시작",
    status: {
      working: "작업 중",
      waiting: "로컬 디렉터리 대기 중",
      completed: "완료됨",
    } as Record<Exclude<RunStatus, "idle">, string>,
    isWaiting: (name: string) => `${name} 로컬 디렉터리 대기 중`,
    branch: "브랜치",
    steps: (file: string) => ["태스크 설명과 댓글 읽기", `${file} 수정`, "변경 내용 정리"],
    folderLabel: "내 폴더",
    noChanges: "변경 없음",
    modified: "수정됨",
    captions: {
      idle: "두 태스크가 같은 로컬 폴더에 있는 웹사이트 문구를 수정해야 합니다.",
      choose:
        "이 폴더는 git 저장소라서 Desktop이 '병렬'을 미리 선택합니다. '직접'을 보려면 먼저 그것을 고른 뒤 '폴더 추가'를 클릭하세요.",
      starting: "운영 도우미가 OPS-21 작업을 시작합니다.",
      ready: "OPS-21이 아직 진행 중입니다. '두 번째 실행 시작'을 클릭해 OPS-22도 같은 폴더를 쓰게 하세요.",
      waiting: "'직접' 모드에서는 한 번에 하나만 실행됩니다. OPS-22는 로컬 디렉터리를 기다리며 폴더를 건드리지 않습니다.",
      handoff: "OPS-21이 끝나 폴더를 놓아주자 OPS-22가 시작됩니다.",
      parallel: "'병렬' 모드에서는 두 실행이 동시에 진행되고, 각자 별도의 워크트리에서 작업합니다. 폴더는 그대로입니다.",
      doneInPlace: "두 실행이 차례로 끝났고, 변경 사항은 폴더에 바로 기록되었습니다. '다시 보기'로 '병렬'도 해 보세요.",
      doneWorktree:
        "각 실행이 agent/… 브랜치를 돌려주었고 폴더는 바뀌지 않았습니다. 브랜치를 검토한 뒤 무엇을 병합할지 정하세요. '다시 보기'로 '직접'도 해 보세요.",
    },
  },
  fr: {
    title: "OPS-21 et OPS-22 · Deux exécutions sur un même dossier local",
    path: "~/Documents/website-copy",
    agent: "Assistant ops",
    issues: [
      { id: "OPS-21", title: "Mettre à jour la page des tarifs", file: "pricing.md", branch: "agent/assistant-ops/ops-21" },
      { id: "OPS-22", title: "Compléter la FAQ", file: "faq.md", branch: "agent/assistant-ops/ops-22" },
    ],
    dialogTitle: "Comment les exécutions doivent-elles utiliser ce dossier ?",
    modes: {
      in_place: {
        title: "Modifier ce dossier directement",
        description:
          "Une exécution à la fois. Les modifications apparaissent dans votre copie de travail au fur et à mesure, prêtes à être reprises.",
      },
      worktree: {
        title: "Exécuter en parallèle, en isolation",
        description:
          "Les exécutions ont lieu simultanément sans toucher à votre copie de travail. Chacune restitue une branche agent/… dans ce dépôt pour relecture et fusion.",
      },
    } as Record<Mode, { title: string; description: string }>,
    add: "Ajouter le dossier",
    badge: { in_place: "Direct", worktree: "Parallèle" } as Record<Mode, string>,
    startSecond: "Lancer la deuxième exécution",
    status: {
      working: "Au travail",
      waiting: "En attente du répertoire local",
      completed: "Terminée",
    } as Record<Exclude<RunStatus, "idle">, string>,
    isWaiting: (name: string) => `${name} attend le répertoire local`,
    branch: "Branche",
    steps: (file: string) => ["Lire la tâche et la discussion", `Modifier ${file}`, "Résumer les modifications"],
    folderLabel: "Votre dossier",
    noChanges: "Aucune modification",
    modified: "Modifié",
    captions: {
      idle: "Deux tâches doivent modifier les textes du site, dans le même dossier local.",
      choose:
        "Ce dossier est un dépôt git, donc Desktop présélectionne Parallèle. Pour voir Direct, choisissez-le d'abord, puis cliquez sur Ajouter le dossier.",
      starting: "Assistant ops commence à travailler sur OPS-21.",
      ready:
        "OPS-21 est toujours en cours. Cliquez sur Lancer la deuxième exécution pour qu'OPS-22 utilise aussi ce dossier.",
      waiting:
        "En mode Direct, une seule exécution à la fois : OPS-22 attend le répertoire local et ne touche pas au dossier.",
      handoff: "OPS-21 est terminée et a libéré le dossier : OPS-22 démarre.",
      parallel:
        "En mode Parallèle, les deux exécutions avancent en même temps, chacune dans son propre worktree. Votre dossier reste tel quel.",
      doneInPlace:
        "Les exécutions se sont succédé et leurs modifications sont écrites directement dans votre dossier. Utilisez Rejouer pour essayer Parallèle.",
      doneWorktree:
        "Chaque exécution a restitué une branche agent/… et votre dossier n'a pas changé. Relisez les branches, puis décidez quoi fusionner. Utilisez Rejouer pour essayer Direct.",
    },
  },
};

type Phase =
  | "idle"
  | "choose"
  | "starting"
  | "ready"
  | "waiting"
  | "handoff"
  | "parallel"
  | "doneInPlace"
  | "doneWorktree";

type Run = { status: RunStatus; steps: number };

const IDLE_RUN: Run = { status: "idle", steps: 0 };

export function LocalDirModeDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Phase>("choose");
  // A git folder on a runtime that can isolate runs opens on worktree.
  const [mode, setMode] = useState<Mode>("worktree");
  const [runs, setRuns] = useState<[Run, Run]>([IDLE_RUN, IDLE_RUN]);
  const [touched, setTouched] = useState<[boolean, boolean]>([false, false]);

  const shownPhase: Phase = visible ? phase : "idle";
  const saved = phase !== "choose";

  const patchRun = (index: 0 | 1, patch: Partial<Run>) =>
    setRuns((current) => {
      const next: [Run, Run] = [current[0], current[1]];
      next[index] = { ...next[index], ...patch };
      return next;
    });

  // In place, the agent's edits show up in the folder while it works.
  const markTouched = (index: 0 | 1) =>
    setTouched((current) => {
      const next: [boolean, boolean] = [current[0], current[1]];
      next[index] = true;
      return next;
    });

  const add = () => {
    setPhase("starting");
    patchRun(0, { status: "working", steps: 1 });
    after(900, () => patchRun(0, { steps: 2 }));
    if (mode === "in_place") after(1200, () => markTouched(0));
    after(1600, () => setPhase("ready"));
  };

  // Runs one issue's remaining steps starting at `start` ms, then completes it.
  const play = (index: 0 | 1, start: number, fromStep: number) => {
    const total = t.steps("").length;
    for (let step = fromStep; step <= total; step++) {
      after(start + (step - fromStep) * 1000, () => {
        patchRun(index, { status: "working", steps: step });
        if (step === 2 && mode === "in_place") after(300, () => markTouched(index));
      });
    }
    const end = start + (total - fromStep + 1) * 1000;
    after(end, () => patchRun(index, { status: "completed" }));
    return end;
  };

  const startSecond = () => {
    if (mode === "in_place") {
      setPhase("waiting");
      patchRun(1, { status: "waiting" });
      const firstDone = play(0, 1200, 3);
      after(firstDone + 600, () => setPhase("handoff"));
      const secondDone = play(1, firstDone + 600, 1);
      after(secondDone + 400, () => setPhase("doneInPlace"));
    } else {
      setPhase("parallel");
      patchRun(1, { status: "working", steps: 1 });
      const firstDone = play(0, 1200, 3);
      const secondDone = play(1, 1000, 2);
      after(Math.max(firstDone, secondDone) + 400, () => setPhase("doneWorktree"));
    }
  };

  const replay = () => {
    clear();
    setPhase("choose");
    setMode("worktree");
    setRuns([IDLE_RUN, IDLE_RUN]);
    setTouched([false, false]);
  };

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="flex min-h-80 flex-col gap-3 p-4">
        {!saved ? (
          <div className="rounded-xl border bg-popover p-4 text-popover-foreground shadow-sm motion-safe:animate-in motion-safe:fade-in">
            <div className="text-title-sm font-medium">{t.dialogTitle}</div>
            <div className="mt-2 rounded-md bg-muted px-2.5 py-1.5 font-mono text-micro break-all text-muted-foreground">
              {t.path}
            </div>
            <div role="radiogroup" aria-label={t.dialogTitle} className="mt-3 flex flex-col gap-2">
              {(["in_place", "worktree"] as Mode[]).map((option) => {
                const selected = mode === option;
                const Icon = option === "in_place" ? Pencil : GitBranch;
                return (
                  <button
                    key={option}
                    type="button"
                    role="radio"
                    aria-checked={selected}
                    disabled={!visible}
                    onClick={() => setMode(option)}
                    className={cn(
                      "flex w-full items-start gap-3 rounded-lg border p-3 text-left transition-colors outline-none focus-visible:ring-3 focus-visible:ring-ring/50",
                      selected ? "border-primary ring-1 ring-primary" : "border-border hover:bg-muted/50",
                    )}
                  >
                    <Icon
                      aria-hidden
                      className={cn("mt-0.5 size-4 shrink-0", selected ? "text-primary" : "text-muted-foreground")}
                    />
                    <span className="min-w-0 flex-1">
                      <span className="flex flex-wrap items-center gap-x-2">
                        <span className="text-body font-medium">{t.modes[option].title}</span>
                        <span className="font-mono text-micro text-muted-foreground">{option}</span>
                      </span>
                      <span className="mt-0.5 block text-caption text-muted-foreground">
                        {t.modes[option].description}
                      </span>
                    </span>
                  </button>
                );
              })}
            </div>
            <div className="mt-3 flex justify-end">
              <CoachMark active={shownPhase === "choose"} side="left" ringClassName="rounded-lg">
                <Button type="button" size="sm" disabled={!visible} onClick={add}>
                  {t.add}
                </Button>
              </CoachMark>
            </div>
          </div>
        ) : (
          <>
            <div className="flex min-w-0 items-center gap-2 rounded-lg border bg-background px-3 py-2 motion-safe:animate-in motion-safe:fade-in">
              <Folder aria-hidden className="size-4 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate font-mono text-caption">{t.path}</span>
              <span className="inline-flex shrink-0 items-center gap-1 rounded-sm bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                {mode === "worktree" ? (
                  <GitBranch aria-hidden className="size-3" />
                ) : (
                  <Pencil aria-hidden className="size-3" />
                )}
                {t.badge[mode]}
              </span>
            </div>

            <div className="grid gap-3 sm:grid-cols-2">
              {t.issues.map((issue, i) => {
                const run = runs[i]!;
                return (
                  <div key={issue.id} className="flex min-w-0 flex-col gap-1.5 rounded-lg border bg-background p-3">
                    <div className="min-w-0 truncate text-label">
                      <span className="text-muted-foreground">{issue.id}</span> {issue.title}
                    </div>
                    {run.status === "idle" ? (
                      <div className="flex min-h-7 items-center">
                        <CoachMark active={shownPhase === "ready"} side="right" ringClassName="rounded-lg">
                          <Button
                            type="button"
                            size="sm"
                            variant="outline"
                            disabled={phase !== "ready"}
                            onClick={startSecond}
                          >
                            {t.startSecond}
                          </Button>
                        </CoachMark>
                      </div>
                    ) : (
                      <RunLane
                        agent={t.agent}
                        run={run}
                        statusLabel={t.status[run.status]}
                        waitingLine={t.isWaiting(t.agent)}
                        steps={t.steps(issue.file)}
                        branch={mode === "worktree" && run.status === "completed" ? issue.branch : null}
                        branchLabel={t.branch}
                      />
                    )}
                  </div>
                );
              })}
            </div>

            <div className="rounded-lg border border-dashed px-3 py-2">
              <div className="text-micro text-muted-foreground">{t.folderLabel}</div>
              {touched.some(Boolean) ? (
                <ul className="m-0 mt-1 flex list-none flex-col gap-0.5 p-0">
                  {t.issues
                    .filter((_, i) => touched[i])
                    .map((issue) => (
                      <li
                        key={issue.file}
                        className="m-0 flex items-center gap-1.5 text-caption motion-safe:animate-in motion-safe:fade-in"
                      >
                        <FileText aria-hidden className="size-3.5 shrink-0 text-muted-foreground" />
                        <span className="font-mono">{issue.file}</span>
                        <span className="text-warning">{t.modified}</span>
                      </li>
                    ))}
                </ul>
              ) : (
                <div className="mt-1 flex items-center gap-1.5 text-caption text-muted-foreground">
                  <Check aria-hidden className="size-3.5 shrink-0 text-success" />
                  {t.noChanges}
                </div>
              )}
            </div>
          </>
        )}
      </div>
    </DemoFrame>
  );
}

function RunLane({
  agent,
  run,
  statusLabel,
  waitingLine,
  steps,
  branch,
  branchLabel,
}: {
  agent: string;
  run: Run;
  statusLabel: string;
  waitingLine: string;
  steps: string[];
  branch: string | null;
  branchLabel: string;
}) {
  const active = run.status === "working";
  return (
    <div className="min-w-0 motion-safe:animate-in motion-safe:fade-in">
      <div className="flex min-h-7 flex-wrap items-center gap-x-2 gap-y-0.5">
        <DemoAvatar name={agent} agent />
        <span className="truncate text-body font-medium">{agent}</span>
        <span
          key={run.status}
          className={cn(
            "inline-flex items-center gap-1 text-caption motion-safe:animate-in motion-safe:fade-in",
            run.status === "waiting" ? "text-warning" : "text-muted-foreground",
          )}
        >
          {run.status === "waiting" ? (
            <Clock aria-hidden className="size-3" />
          ) : active ? (
            <Loader2 aria-hidden className="size-3 text-brand motion-safe:animate-spin" />
          ) : (
            <Check aria-hidden className="size-3 text-success" />
          )}
          {statusLabel}
        </span>
      </div>
      {run.status === "waiting" ? (
        <p className="mt-1 pl-7 text-caption text-muted-foreground">{waitingLine}</p>
      ) : (
        <ul className="mt-1 flex flex-col gap-0.5 pl-7">
          {steps.slice(0, run.steps).map((step, i) => (
            <DemoStep key={step} state={i === run.steps - 1 && active ? "running" : "done"}>
              {step}
            </DemoStep>
          ))}
        </ul>
      )}
      {branch && (
        <div className="mt-1.5 flex min-w-0 items-center gap-1.5 pl-7 text-caption motion-safe:animate-in motion-safe:fade-in">
          <GitBranch aria-hidden className="size-3.5 shrink-0 text-brand" />
          <span className="text-muted-foreground">{branchLabel}</span>
          <code className="min-w-0 truncate rounded-xs bg-muted px-1 font-mono text-micro">{branch}</code>
        </div>
      )}
    </div>
  );
}
