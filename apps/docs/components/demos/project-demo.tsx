"use client";

import { useState } from "react";
import { ChevronDown, FolderKanban } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoAvatar, DemoFrame, useDemoCopy, useStartWhenVisible } from "./demo-kit";

type IssueStatus = "todo" | "in_progress" | "in_review" | "done" | "cancelled";
type ProjectStatus = "planned" | "in_progress" | "paused" | "completed" | "cancelled";
type IssueId = "OPS-41" | "OPS-42" | "OPS-43" | "OPS-44";

const ISSUE_MENU: IssueStatus[] = ["todo", "in_progress", "in_review", "done", "cancelled"];
const PROJECT_MENU: ProjectStatus[] = ["planned", "in_progress", "paused", "completed", "cancelled"];

// Progress counts issues in the done and closed categories (handler/project.go).
const finished = (s: IssueStatus) => s === "done" || s === "cancelled";

const issueDot: Record<IssueStatus, string> = {
  todo: "border-muted-foreground",
  in_progress: "border-warning bg-warning/40",
  in_review: "border-brand bg-brand/40",
  done: "border-success bg-success",
  cancelled: "border-muted-foreground bg-muted-foreground/50",
};

const copy = {
  en: {
    title: "Project · Anniversary online event",
    project: "Anniversary online event",
    statusLabel: "Status",
    leadLabel: "Lead",
    progressLabel: "Progress",
    you: "You",
    descriptionLabel: "Description",
    description: "Run the anniversary online event by October 31: the announcement post, the prize draw and partner promotion. Keep the brand voice friendly; budget stays within the approved amount.",
    descriptionHint: "Used as project context for agents, applied to every run in this project.",
    issueStatus: {
      todo: "Todo",
      in_progress: "In Progress",
      in_review: "In Review",
      done: "Done",
      cancelled: "Cancelled",
    } as Record<IssueStatus, string>,
    projectStatus: {
      planned: "Planned",
      in_progress: "In Progress",
      paused: "Paused",
      completed: "Completed",
      cancelled: "Cancelled",
    } as Record<ProjectStatus, string>,
    issues: {
      "OPS-41": "Pick the event theme and dates",
      "OPS-42": "Write the event announcement post",
      "OPS-43": "List the prizes for the draw",
      "OPS-44": "Book a partner influencer",
    } as Record<IssueId, string>,
    captions: {
      idle: "A project with four issues; two are finished.",
      first: "OPS-42 has been reviewed. Click its status and mark it Done.",
      firstMenu: "Pick Done.",
      second: "Progress went up a step. The partner promotion is no longer happening: set OPS-44 to Cancelled.",
      secondMenu: "Pick Cancelled. A cancelled issue has left the project's scope.",
      allDone: "Cancelled counts toward progress too, so the project is at 100%. Its own status is still In Progress: it does not follow the issues. Click the project status.",
      projectMenu: "Pick Completed. Changing it would not touch any issue either.",
      done: "The project is now Completed; that never happens on its own. Use Replay to watch again.",
    },
  },
  zh: {
    title: "项目 · 周年庆线上活动",
    project: "周年庆线上活动",
    statusLabel: "状态",
    leadLabel: "负责人",
    progressLabel: "进度",
    you: "你",
    descriptionLabel: "描述",
    description: "10 月 31 日前办完周年庆线上活动：活动推文、抽奖和合作推广。语气保持亲切，花费不超过已批预算。",
    descriptionHint: "会作为项目上下文提供给智能体，应用于该项目下的每次运行。",
    issueStatus: {
      todo: "待办",
      in_progress: "进行中",
      in_review: "审核中",
      done: "已完成",
      cancelled: "已取消",
    } as Record<IssueStatus, string>,
    projectStatus: {
      planned: "计划中",
      in_progress: "进行中",
      paused: "已暂停",
      completed: "已完成",
      cancelled: "已取消",
    } as Record<ProjectStatus, string>,
    issues: {
      "OPS-41": "确定活动主题和时间",
      "OPS-42": "写活动推文",
      "OPS-43": "列出抽奖奖品清单",
      "OPS-44": "联系合作博主",
    } as Record<IssueId, string>,
    captions: {
      idle: "一个包含 4 条任务的项目，其中 2 条已经结束。",
      first: "OPS-42 已经检查过了。点击它的状态，改为已完成。",
      firstMenu: "选择已完成。",
      second: "进度前进了一格。合作推广不做了：把 OPS-44 改为已取消。",
      secondMenu: "选择已取消。取消表示这项工作已从项目范围中结束。",
      allDone: "已取消同样计入进度，项目到了 100%，但项目状态还是进行中：它不跟随任务变化。点击项目状态。",
      projectMenu: "选择已完成。修改项目状态也不会改动其中的任务。",
      done: "项目状态改成了已完成，这一步不会自动发生。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "プロジェクト · 周年記念オンラインイベント",
    project: "周年記念オンラインイベント",
    statusLabel: "ステータス",
    leadLabel: "リード",
    progressLabel: "進捗",
    you: "あなた",
    descriptionLabel: "説明",
    description: "10 月 31 日までに周年記念オンラインイベントを実施する：告知投稿、抽選、パートナーとの共同プロモーション。トーンは親しみやすく、費用は承認済みの予算内に収める。",
    descriptionHint: "このプロジェクトのすべての実行で、コンテキストとしてエージェントに共有されます。",
    issueStatus: {
      todo: "未着手",
      in_progress: "進行中",
      in_review: "レビュー中",
      done: "完了",
      cancelled: "キャンセル済み",
    } as Record<IssueStatus, string>,
    projectStatus: {
      planned: "計画中",
      in_progress: "進行中",
      paused: "一時停止",
      completed: "完了",
      cancelled: "キャンセル",
    } as Record<ProjectStatus, string>,
    issues: {
      "OPS-41": "イベントのテーマと日程を決める",
      "OPS-42": "イベント告知の投稿を書く",
      "OPS-43": "抽選の景品リストを作る",
      "OPS-44": "パートナーのインフルエンサーに依頼する",
    } as Record<IssueId, string>,
    captions: {
      idle: "4 件のタスクを含むプロジェクト。そのうち 2 件はすでに終わっています。",
      first: "OPS-42 はレビュー済みです。ステータスをクリックして完了にしてください。",
      firstMenu: "完了を選んでください。",
      second: "進捗が 1 段進みました。パートナーとの共同プロモーションは取りやめになったので、OPS-44 をキャンセル済みにしてください。",
      secondMenu: "キャンセル済みを選んでください。キャンセルは、その作業がプロジェクトの範囲から外れたことを表します。",
      allDone: "キャンセル済みも進捗に数えるので、プロジェクトは 100% になりました。それでもプロジェクトのステータスは進行中のままで、タスクには連動しません。プロジェクトのステータスをクリックしてください。",
      projectMenu: "完了を選んでください。プロジェクトのステータスを変えても、タスクは変わりません。",
      done: "プロジェクトが完了になりました。これは自動では起きません。右上から最初に戻せます。",
    },
  },
  ko: {
    title: "프로젝트 · 창립 기념 온라인 이벤트",
    project: "창립 기념 온라인 이벤트",
    statusLabel: "상태",
    leadLabel: "리드",
    progressLabel: "진행률",
    you: "나",
    descriptionLabel: "설명",
    description: "10월 31일까지 창립 기념 온라인 이벤트를 마친다: 이벤트 공지 게시물, 경품 추첨, 파트너 공동 홍보. 말투는 친근하게 유지하고 비용은 승인된 예산을 넘지 않는다.",
    descriptionHint: "이 프로젝트의 모든 실행에서 컨텍스트로 에이전트에게 공유됩니다.",
    issueStatus: {
      todo: "할 일",
      in_progress: "진행 중",
      in_review: "리뷰 중",
      done: "완료",
      cancelled: "취소됨",
    } as Record<IssueStatus, string>,
    projectStatus: {
      planned: "계획됨",
      in_progress: "진행 중",
      paused: "일시 중지",
      completed: "완료됨",
      cancelled: "취소됨",
    } as Record<ProjectStatus, string>,
    issues: {
      "OPS-41": "이벤트 주제와 일정 정하기",
      "OPS-42": "이벤트 공지 게시물 작성",
      "OPS-43": "추첨 경품 목록 만들기",
      "OPS-44": "협업 인플루언서 섭외",
    } as Record<IssueId, string>,
    captions: {
      idle: "태스크 4개가 있는 프로젝트입니다. 그중 2개는 이미 끝났습니다.",
      first: "OPS-42는 검토를 마쳤습니다. 상태를 클릭해 완료로 바꾸세요.",
      firstMenu: "완료를 선택하세요.",
      second: "진행률이 한 칸 올랐습니다. 파트너 공동 홍보는 하지 않기로 했으니 OPS-44를 취소됨으로 바꾸세요.",
      secondMenu: "취소됨을 선택하세요. 취소는 그 작업이 프로젝트 범위에서 끝났다는 뜻입니다.",
      allDone: "취소됨도 진행률에 들어가서 프로젝트가 100%가 됐습니다. 그래도 프로젝트 상태는 여전히 진행 중입니다. 태스크를 따라 바뀌지 않습니다. 프로젝트 상태를 클릭하세요.",
      projectMenu: "완료됨을 선택하세요. 프로젝트 상태를 바꿔도 태스크는 바뀌지 않습니다.",
      done: "프로젝트가 완료됨이 됐습니다. 이 단계는 자동으로 일어나지 않습니다. 오른쪽 위에서 다시 볼 수 있습니다.",
    },
  },
  fr: {
    title: "Projet · Événement en ligne de l'anniversaire",
    project: "Événement en ligne de l'anniversaire",
    statusLabel: "Statut",
    leadLabel: "Responsable",
    progressLabel: "Avancement",
    you: "Vous",
    descriptionLabel: "Description",
    description: "Organiser l'événement en ligne de l'anniversaire d'ici le 31 octobre : la publication d'annonce, le tirage au sort et la promotion avec un partenaire. Garder un ton chaleureux ; ne pas dépasser le budget validé.",
    descriptionHint: "Partagé avec les agents comme contexte à chaque exécution dans ce projet.",
    issueStatus: {
      todo: "À faire",
      in_progress: "En cours",
      in_review: "En relecture",
      done: "Terminé",
      cancelled: "Annulé",
    } as Record<IssueStatus, string>,
    projectStatus: {
      planned: "Planifié",
      in_progress: "En cours",
      paused: "En pause",
      completed: "Terminé",
      cancelled: "Annulé",
    } as Record<ProjectStatus, string>,
    issues: {
      "OPS-41": "Choisir le thème et les dates de l'événement",
      "OPS-42": "Rédiger la publication d'annonce",
      "OPS-43": "Lister les lots du tirage au sort",
      "OPS-44": "Contacter un influenceur partenaire",
    } as Record<IssueId, string>,
    captions: {
      idle: "Un projet de quatre tâches, dont deux sont déjà closes.",
      first: "OPS-42 a été relue. Cliquez sur son statut et passez-la à Terminé.",
      firstMenu: "Choisissez Terminé.",
      second: "L'avancement a progressé d'un cran. La promotion avec le partenaire n'aura pas lieu : passez OPS-44 à Annulé.",
      secondMenu: "Choisissez Annulé. Une tâche annulée est sortie du périmètre du projet.",
      allDone: "Annulé compte aussi dans l'avancement : le projet est à 100 %. Son propre statut reste En cours, car il ne suit pas les tâches. Cliquez sur le statut du projet.",
      projectMenu: "Choisissez Terminé. Changer le statut du projet ne touche à aucune tâche non plus.",
      done: "Le projet est maintenant Terminé ; cela n'arrive jamais tout seul. Relancez la démo pour la revoir.",
    },
  },
};

type Phase = "idle" | "first" | "firstMenu" | "second" | "secondMenu" | "allDone" | "projectMenu" | "done";

const INITIAL: Record<IssueId, IssueStatus> = {
  "OPS-41": "done",
  "OPS-42": "in_review",
  "OPS-43": "done",
  "OPS-44": "todo",
};

// Which issue each phase works on, and the status its menu accepts.
const SCRIPT: Partial<Record<Phase, { issue: IssueId; to: IssueStatus; menu: boolean }>> = {
  first: { issue: "OPS-42", to: "done", menu: false },
  firstMenu: { issue: "OPS-42", to: "done", menu: true },
  second: { issue: "OPS-44", to: "cancelled", menu: false },
  secondMenu: { issue: "OPS-44", to: "cancelled", menu: true },
};

export function ProjectDemo() {
  const t = useDemoCopy(copy);
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("first");
  const [statuses, setStatuses] = useState(INITIAL);
  const [projectStatus, setProjectStatus] = useState<ProjectStatus>("in_progress");

  const shownPhase: Phase = visible ? phase : "idle";
  const ids = Object.keys(statuses) as IssueId[];
  const done = ids.filter((id) => finished(statuses[id])).length;
  const percent = Math.round((done / ids.length) * 100);
  const step = visible ? SCRIPT[phase] : undefined;

  const toggleMenu = () => {
    if (phase === "first") setPhase("firstMenu");
    else if (phase === "firstMenu") setPhase("first");
    else if (phase === "second") setPhase("secondMenu");
    else if (phase === "secondMenu") setPhase("second");
  };

  const pickIssue = (id: IssueId, to: IssueStatus) => {
    setStatuses((current) => ({ ...current, [id]: to }));
    setPhase(id === "OPS-42" ? "second" : "allDone");
  };

  const replay = () => {
    setPhase("first");
    setStatuses(INITIAL);
    setProjectStatus("in_progress");
  };

  const projectMenuOpen = phase === "projectMenu";

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="flex min-h-96 flex-col gap-3 p-4">
        <div className="flex items-center gap-2">
          <FolderKanban aria-hidden className="size-4 text-muted-foreground" />
          <span className="text-body font-medium">{t.project}</span>
        </div>

        <dl className="grid grid-cols-[auto_1fr] items-center gap-x-6 gap-y-2 text-body">
          <dt className="text-caption text-muted-foreground">{t.statusLabel}</dt>
          <dd className="relative">
            <CoachMark active={visible && phase === "allDone"} side="right" ringClassName="rounded-md">
              <button
                type="button"
                aria-haspopup="listbox"
                aria-expanded={projectMenuOpen}
                disabled={!visible || (phase !== "allDone" && phase !== "projectMenu")}
                onClick={() => setPhase(projectMenuOpen ? "allDone" : "projectMenu")}
                className="-mx-1.5 inline-flex h-7 items-center gap-1.5 rounded-md px-1.5 hover:bg-muted disabled:hover:bg-transparent"
              >
                <span key={projectStatus} className="motion-safe:animate-in motion-safe:fade-in">
                  {t.projectStatus[projectStatus]}
                </span>
                {(phase === "allDone" || projectMenuOpen) && (
                  <ChevronDown aria-hidden className="size-3.5 text-muted-foreground" />
                )}
              </button>
            </CoachMark>
            {projectMenuOpen && (
              <div
                role="listbox"
                aria-label={t.statusLabel}
                className="absolute top-full left-0 z-10 mt-1 w-40 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
              >
                {PROJECT_MENU.map((s) => (
                  <CoachMark quiet key={s} active={s === "completed"} ringClassName="rounded-md" className="flex w-full">
                    <button
                      type="button"
                      role="option"
                      aria-selected={s === projectStatus}
                      disabled={s !== "completed"}
                      onClick={() => {
                        setProjectStatus("completed");
                        setPhase("done");
                      }}
                      className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-body hover:bg-accent disabled:opacity-45 disabled:hover:bg-transparent"
                    >
                      <span className="min-w-0 flex-1 truncate">{t.projectStatus[s]}</span>
                      {s === projectStatus && <span className="text-caption text-muted-foreground">✓</span>}
                    </button>
                  </CoachMark>
                ))}
              </div>
            )}
          </dd>
          <dt className="text-caption text-muted-foreground">{t.leadLabel}</dt>
          <dd className="flex items-center gap-1.5">
            <DemoAvatar name={t.you} />
            {t.you}
          </dd>
          <dt className="text-caption text-muted-foreground">{t.progressLabel}</dt>
          <dd className="flex min-w-0 items-center gap-2">
            <span
              role="progressbar"
              aria-label={t.progressLabel}
              aria-valuenow={percent}
              aria-valuemin={0}
              aria-valuemax={100}
              className="h-1.5 w-full max-w-40 overflow-hidden rounded-full bg-muted"
            >
              <span
                className="block h-full rounded-full bg-success transition-[width] duration-500"
                style={{ width: `${percent}%` }}
              />
            </span>
            <span key={done} className="shrink-0 text-caption tabular-nums text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
              {done}/{ids.length} · {percent}%
            </span>
          </dd>
        </dl>

        <ul className="rounded-lg border bg-background">
          {ids.map((id) => {
            const s = statuses[id];
            const active = step?.issue === id;
            const menuOpen = active && step.menu;
            return (
              <li key={id} className="relative m-0 flex min-h-10 items-center gap-2 border-b px-3 py-1.5 last:border-b-0">
                <span className="w-14 shrink-0 text-caption tabular-nums text-muted-foreground">{id}</span>
                <span
                  className={cn(
                    "min-w-0 flex-1 truncate text-body",
                    s === "cancelled" && "text-muted-foreground line-through",
                  )}
                >
                  {t.issues[id]}
                </span>
                <CoachMark quiet active={active && !step.menu} ringClassName="rounded-md">
                  <button
                    type="button"
                    aria-haspopup="listbox"
                    aria-expanded={menuOpen}
                    disabled={!active}
                    onClick={toggleMenu}
                    className="inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md px-1.5 text-caption hover:bg-muted disabled:hover:bg-transparent"
                  >
                    <span aria-hidden className={cn("size-2.5 rounded-full border-2", issueDot[s])} />
                    <span key={s} className="motion-safe:animate-in motion-safe:fade-in">
                      {t.issueStatus[s]}
                    </span>
                    {active && <ChevronDown aria-hidden className="size-3 text-muted-foreground" />}
                  </button>
                </CoachMark>
                {menuOpen && (
                  <div
                    role="listbox"
                    aria-label={t.statusLabel}
                    className={cn(
                      "absolute right-2 z-10 w-36 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in",
                      // The last row opens upward so the menu stays inside the frame.
                      id === ids[ids.length - 1] ? "bottom-full mb-1" : "top-full mt-1",
                    )}
                  >
                    {ISSUE_MENU.map((option) => (
                      <CoachMark quiet key={option} active={option === step.to} ringClassName="rounded-md" className="flex w-full">
                        <button
                          type="button"
                          role="option"
                          aria-selected={option === s}
                          disabled={option !== step.to}
                          onClick={() => pickIssue(id, option)}
                          className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-body hover:bg-accent disabled:opacity-45 disabled:hover:bg-transparent"
                        >
                          <span aria-hidden className={cn("size-3 shrink-0 rounded-full border-2", issueDot[option])} />
                          <span className="min-w-0 flex-1 truncate">{t.issueStatus[option]}</span>
                          {option === s && <span className="text-caption text-muted-foreground">✓</span>}
                        </button>
                      </CoachMark>
                    ))}
                  </div>
                )}
              </li>
            );
          })}
        </ul>

        <div className="rounded-lg bg-muted/40 px-3 py-2">
          <div className="text-caption text-muted-foreground">{t.descriptionLabel}</div>
          <p className="mt-0.5 text-caption">{t.description}</p>
          <p className="mt-1 text-micro text-muted-foreground">{t.descriptionHint}</p>
        </div>
      </div>
    </DemoFrame>
  );
}
