"use client";

import { useState, type ReactNode } from "react";
import {
  CheckCircle2,
  CircleDashed,
  CircleSlash,
  GitMerge,
  GitPullRequestArrow,
  MoreHorizontal,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { CoachMark, DemoAvatar, DemoFrame, useDemoCopy, useDemoTimers, useStartWhenVisible } from "./demo-kit";

type Status = "in_review" | "done";

const copy = {
  en: {
    title: "OPS-31 · Update the campaign copy on the website homepage",
    github: "GitHub",
    multica: "Multica",
    ghTitle: "OPS-31 Update the campaign copy on the website homepage",
    ghNumber: "#19",
    ghBranch: "ops-31-homepage-copy → main",
    open: "Open",
    merged: "Merged",
    checks: "All checks have passed · No conflicts",
    mergeButton: "Merge pull request",
    merging: "Merging...",
    mergedNote: "Pull request successfully merged",
    issueTitle: "OPS-31 · Update the campaign copy on the website homepage",
    statusLabel: "Status",
    status: { in_review: "In Review", done: "Done" } as Record<Status, string>,
    section: "Pull requests",
    sectionMenu: "Pull request automation",
    disable: "Keep status when PRs merge",
    prTitle: "Update the campaign copy on the website homepage",
    diff: { add: "+12", del: "−4" },
    justNow: "just now",
    ready: "Ready",
    waiting: { before: "Moves to ", after: " when #19 merges" },
    issueDisabled: "PR merges won’t change this issue",
    issueDisabledAction: "Turn on",
    you: "You",
    optedOutActivity: "set this issue to keep its status when PRs merge",
    mergedActivity: "Changed status from In Review to Done after every linked PR merged (#19)",
    captions: {
      idle: "An issue with a linked pull request that hasn't merged yet.",
      ready:
        "The issue is linked to #19, and the line under it says what a merge will do. Click Merge pull request on GitHub, or first use the ⋯ menu to keep this issue's status.",
      menu: "Keep status when PRs merge: this issue stays where it is after its PRs merge.",
      optedOut: "This issue now keeps its status when PRs merge. Merge on GitHub, or click Turn on to go back to the default.",
      merging: "GitHub sends the merge event to Multica.",
      merged:
        "Every linked PR has merged, so the issue moved to Done and the timeline records it as a system change. Use Replay to try the other path.",
      kept: "The PR merged, but this issue keeps its status when PRs merge, so it stays In Review. Use Replay to start over.",
    },
  },
  zh: {
    title: "OPS-31 · 更新官网首页的活动文案",
    github: "GitHub",
    multica: "Multica",
    ghTitle: "OPS-31 更新官网首页的活动文案",
    ghNumber: "#19",
    ghBranch: "ops-31-homepage-copy → main",
    open: "Open",
    merged: "Merged",
    checks: "检查已通过，没有冲突",
    mergeButton: "Merge pull request",
    merging: "合并中...",
    mergedNote: "Pull request 已合并",
    issueTitle: "OPS-31 · 更新官网首页的活动文案",
    statusLabel: "状态",
    status: { in_review: "审核中", done: "已完成" } as Record<Status, string>,
    section: "Pull Request",
    sectionMenu: "PR 自动化",
    disable: "此任务合并后不改状态",
    prTitle: "更新官网首页的活动文案",
    diff: { add: "+12", del: "−4" },
    justNow: "刚刚",
    ready: "可合入",
    waiting: { before: "等待 #19 合并后改为 ", after: "" },
    issueDisabled: "此任务合并后不改状态",
    issueDisabledAction: "恢复",
    you: "你",
    optedOutActivity: "设置此任务合并后不改状态",
    mergedActivity: "所有关联 PR 均已合并（#19），状态从 审核中 改为 已完成",
    captions: {
      idle: "一条关联了 PR 的任务，PR 还没有合并。",
      ready: "任务关联了 #19，列表下方写着合并后会改成什么状态。在 GitHub 上点击 Merge pull request；也可以先在 ⋯ 菜单里让这条任务合并后不改状态。",
      menu: "选择\"此任务合并后不改状态\"后，这条任务的 PR 合并时保持当前状态。",
      optedOut: "这条任务已设置为合并后不改状态。去 GitHub 合并，或点击\"恢复\"回到默认规则。",
      merging: "GitHub 把合并事件发给 Multica。",
      merged: "关联的 PR 全部合并，任务改为已完成，时间线以系统操作记下这次变化。点右上角可以换一条路径再试。",
      kept: "PR 已合并，但这条任务设置了合并后不改状态，所以仍是审核中。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "OPS-31 · Web サイトのトップページのキャンペーン文言を更新する",
    github: "GitHub",
    multica: "Multica",
    ghTitle: "OPS-31 Web サイトのトップページのキャンペーン文言を更新する",
    ghNumber: "#19",
    ghBranch: "ops-31-homepage-copy → main",
    open: "Open",
    merged: "Merged",
    checks: "すべてのチェックに合格 · コンフリクトなし",
    mergeButton: "Merge pull request",
    merging: "マージ中...",
    mergedNote: "Pull request をマージしました",
    issueTitle: "OPS-31 · Web サイトのトップページのキャンペーン文言を更新する",
    statusLabel: "ステータス",
    status: { in_review: "レビュー中", done: "完了" } as Record<Status, string>,
    section: "Pull requests",
    sectionMenu: "PR の自動化",
    disable: "PR マージ後もステータスを変えない",
    prTitle: "Web サイトのトップページのキャンペーン文言を更新する",
    diff: { add: "+12", del: "−4" },
    justNow: "たった今",
    ready: "マージ可能",
    waiting: { before: "#19 がマージされると ", after: " に変更" },
    issueDisabled: "このタスクは PR マージでステータスを変えません",
    issueDisabledAction: "再開",
    you: "あなた",
    optedOutActivity: "このタスクを PR マージでステータスを変えないように設定しました",
    mergedActivity: "関連するすべての PR がマージされたため（#19）、ステータスを レビュー中 から 完了 に変更しました",
    captions: {
      idle: "PR が関連付けられたタスクです。PR はまだマージされていません。",
      ready:
        "タスクには #19 が関連付けられ、その下の行にマージ後のステータスが表示されています。GitHub で Merge pull request をクリックするか、先に ⋯ メニューでこのタスクのステータスを変えないように設定します。",
      menu: "「PR マージ後もステータスを変えない」を選ぶと、このタスクは PR がマージされても今のステータスのままです。",
      optedOut: "このタスクは PR マージでステータスを変えない設定になりました。GitHub でマージするか、「再開」をクリックしてデフォルトに戻します。",
      merging: "GitHub がマージイベントを Multica に送ります。",
      merged:
        "関連する PR がすべてマージされたので、タスクは 完了 になり、タイムラインにシステムによる変更として記録されました。「もう一度」で別の流れも試せます。",
      kept: "PR はマージされましたが、このタスクは PR マージでステータスを変えない設定なので レビュー中 のままです。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "OPS-31 · 웹사이트 홈페이지의 캠페인 문구 업데이트",
    github: "GitHub",
    multica: "Multica",
    ghTitle: "OPS-31 웹사이트 홈페이지의 캠페인 문구 업데이트",
    ghNumber: "#19",
    ghBranch: "ops-31-homepage-copy → main",
    open: "Open",
    merged: "Merged",
    checks: "모든 검사 통과 · 충돌 없음",
    mergeButton: "Merge pull request",
    merging: "병합 중...",
    mergedNote: "Pull request가 병합되었습니다",
    issueTitle: "OPS-31 · 웹사이트 홈페이지의 캠페인 문구 업데이트",
    statusLabel: "상태",
    status: { in_review: "리뷰 중", done: "완료" } as Record<Status, string>,
    section: "Pull requests",
    sectionMenu: "PR 자동화",
    disable: "PR 병합 후에도 상태 유지",
    prTitle: "웹사이트 홈페이지의 캠페인 문구 업데이트",
    diff: { add: "+12", del: "−4" },
    justNow: "방금 전",
    ready: "병합 가능",
    waiting: { before: "#19 병합 시 ", after: "로 변경" },
    issueDisabled: "이 태스크는 PR 병합 시 상태를 바꾸지 않음",
    issueDisabledAction: "다시 켜기",
    you: "나",
    optedOutActivity: "이 태스크가 PR 병합 시 상태를 유지하도록 설정했습니다",
    mergedActivity: "연결된 PR이 모두 병합되어(#19) 상태를 리뷰 중에서 완료로 변경했습니다",
    captions: {
      idle: "PR이 연결된 태스크입니다. PR은 아직 병합되지 않았습니다.",
      ready:
        "태스크에 #19가 연결되어 있고, 그 아래 줄에 병합 후 바뀔 상태가 표시됩니다. GitHub에서 Merge pull request를 클릭하거나, 먼저 ⋯ 메뉴에서 이 태스크의 상태를 유지하도록 설정하세요.",
      menu: "PR 병합 후에도 상태 유지를 선택하면 이 태스크는 PR이 병합되어도 현재 상태를 유지합니다.",
      optedOut: "이제 이 태스크는 PR 병합 시 상태를 유지합니다. GitHub에서 병합하거나 다시 켜기를 클릭해 기본 동작으로 돌아가세요.",
      merging: "GitHub가 병합 이벤트를 Multica로 보냅니다.",
      merged:
        "연결된 PR이 모두 병합되어 태스크가 완료로 바뀌었고, 타임라인에 시스템 변경으로 기록되었습니다. 다시 보기를 눌러 다른 경로도 시도해 보세요.",
      kept: "PR은 병합되었지만 이 태스크는 PR 병합 시 상태를 유지하도록 설정되어 있어 리뷰 중으로 남습니다. 다시 보기를 누르면 처음부터 볼 수 있습니다.",
    },
  },
  fr: {
    title: "OPS-31 · Mettre à jour le texte de la campagne sur la page d'accueil du site",
    github: "GitHub",
    multica: "Multica",
    ghTitle: "OPS-31 Mettre à jour le texte de la campagne sur la page d'accueil du site",
    ghNumber: "#19",
    ghBranch: "ops-31-homepage-copy → main",
    open: "Open",
    merged: "Merged",
    checks: "Toutes les vérifications ont réussi · Aucun conflit",
    mergeButton: "Merge pull request",
    merging: "Fusion...",
    mergedNote: "Pull request fusionnée",
    issueTitle: "OPS-31 · Mettre à jour le texte de la campagne sur la page d'accueil du site",
    statusLabel: "Statut",
    status: { in_review: "En relecture", done: "Terminé" } as Record<Status, string>,
    section: "Pull requests",
    sectionMenu: "Automatisation des pull requests",
    disable: "Garder le statut à la fusion des PR",
    prTitle: "Mettre à jour le texte de la campagne sur la page d'accueil du site",
    diff: { add: "+12", del: "−4" },
    justNow: "à l'instant",
    ready: "Prête à fusionner",
    waiting: { before: "Passe à ", after: " quand #19 est fusionnée" },
    issueDisabled: "Les fusions de PR ne changent pas cette tâche",
    issueDisabledAction: "Réactiver",
    you: "Vous",
    optedOutActivity: "a configuré cette tâche pour garder son statut à la fusion des PR",
    mergedActivity: "Statut changé de En relecture à Terminé après la fusion de toutes les PR liées (#19)",
    captions: {
      idle: "Une tâche liée à une pull request qui n'est pas encore fusionnée.",
      ready:
        "La tâche est liée à #19, et la ligne en dessous indique ce que fera la fusion. Cliquez sur Merge pull request sur GitHub, ou passez d'abord par le menu ⋯ pour que cette tâche garde son statut.",
      menu: "Garder le statut à la fusion des PR : cette tâche reste où elle est après la fusion de ses PR.",
      optedOut:
        "Cette tâche garde désormais son statut à la fusion des PR. Fusionnez sur GitHub, ou cliquez sur Réactiver pour revenir au comportement par défaut.",
      merging: "GitHub envoie l'événement de fusion à Multica.",
      merged:
        "Toutes les PR liées sont fusionnées : la tâche passe à Terminé, et la chronologie l'enregistre comme une modification système. Cliquez sur Rejouer pour essayer l'autre chemin.",
      kept: "La PR est fusionnée, mais cette tâche garde son statut à la fusion des PR : elle reste En relecture. Cliquez sur Rejouer pour recommencer.",
    },
  },
};

type Phase = "idle" | "ready" | "menu" | "optedOut" | "merging" | "merged" | "kept";

const statusDot: Record<Status, string> = {
  in_review: "border-brand bg-brand/40",
  done: "border-success bg-success",
};

function StatusChip({ label, status }: { label: string; status: Status }) {
  return (
    <span className="inline-flex items-center gap-1 align-[-2px] font-medium text-foreground">
      <span aria-hidden className={cn("size-2.5 rounded-full border-2", statusDot[status])} />
      {label}
    </span>
  );
}

export function PrLinkDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("ready");
  const [optedOut, setOptedOut] = useState(false);
  const [status, setStatus] = useState<Status>("in_review");

  const shownPhase: Phase = visible ? phase : "idle";
  const prMerged = phase === "merged" || phase === "kept";
  const canMerge = visible && (phase === "ready" || phase === "optedOut");
  const canUseMenu = visible && (phase === "ready" || phase === "menu");

  const merge = () => {
    const keep = optedOut;
    setPhase("merging");
    after(900, () => {
      setPhase(keep ? "kept" : "merged");
      if (!keep) after(500, () => setStatus("done"));
    });
  };

  const optOut = () => {
    setOptedOut(true);
    setPhase("optedOut");
  };

  const turnBackOn = () => {
    setOptedOut(false);
    setPhase("ready");
  };

  const replay = () => {
    clear();
    setPhase("ready");
    setOptedOut(false);
    setStatus("in_review");
  };

  // The line under the PR list follows the server's decision: it names the
  // target while a PR is open, reports an opt-out, and goes quiet once the
  // issue is done.
  let autoLine: ReactNode = null;
  if (optedOut) {
    autoLine = (
      <>
        <CircleSlash aria-hidden />
        <span className="min-w-0">
          {t.issueDisabled} ·{" "}
          <button
            type="button"
            disabled={phase !== "optedOut"}
            onClick={turnBackOn}
            className="font-medium text-foreground underline decoration-border underline-offset-2 hover:decoration-foreground disabled:no-underline"
          >
            {t.issueDisabledAction}
          </button>
        </span>
      </>
    );
  } else if (!prMerged) {
    autoLine = (
      <>
        <CircleDashed aria-hidden />
        <span className="min-w-0">
          {t.waiting.before}
          <StatusChip label={t.status.done} status="done" />
          {t.waiting.after}
        </span>
      </>
    );
  }

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="grid min-h-80 gap-3 p-4 sm:grid-cols-2">
        <section className="flex min-w-0 flex-col gap-2 rounded-lg border bg-background p-3">
          <div className="text-micro font-medium text-muted-foreground">{t.github}</div>
          <div className="text-body font-medium break-words">
            {t.ghTitle} <span className="font-normal text-muted-foreground">{t.ghNumber}</span>
          </div>
          <div className="flex min-w-0 flex-wrap items-center gap-2 text-caption text-muted-foreground">
            <span
              key={String(prMerged)}
              className={cn(
                "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-micro font-medium text-white motion-safe:animate-in motion-safe:fade-in",
                prMerged ? "bg-violet-600" : "bg-emerald-600",
              )}
            >
              {prMerged ? <GitMerge aria-hidden className="size-3" /> : <GitPullRequestArrow aria-hidden className="size-3" />}
              {prMerged ? t.merged : t.open}
            </span>
            <span className="min-w-0 truncate font-mono text-micro">{t.ghBranch}</span>
          </div>
          <div className="mt-auto flex flex-col gap-2 rounded-md border bg-muted/40 p-2.5">
            {prMerged ? (
              <span className="flex items-center gap-1.5 text-caption motion-safe:animate-in motion-safe:fade-in">
                <GitMerge aria-hidden className="size-3.5 text-violet-600 dark:text-violet-400" />
                {t.mergedNote}
              </span>
            ) : (
              <>
                <span className="flex items-center gap-1.5 text-caption">
                  <CheckCircle2 aria-hidden className="size-3.5 text-success" />
                  {t.checks}
                </span>
                <CoachMark active={canMerge} side="bottom" align="start" ringClassName="rounded-lg">
                  <Button type="button" size="sm" disabled={!canMerge} onClick={merge}>
                    {phase === "merging" ? t.merging : t.mergeButton}
                  </Button>
                </CoachMark>
              </>
            )}
          </div>
        </section>

        <section className="flex min-w-0 flex-col gap-3 rounded-lg border bg-background p-3">
          <div className="text-micro font-medium text-muted-foreground">{t.multica}</div>
          <div className="text-body font-medium break-words">{t.issueTitle}</div>
          <div className="flex items-center gap-3 text-body">
            <span className="text-caption text-muted-foreground">{t.statusLabel}</span>
            <span className="flex items-center gap-1.5">
              <span aria-hidden className={cn("size-3 rounded-full border-2 transition-colors", statusDot[status])} />
              <span key={status} className="motion-safe:animate-in motion-safe:fade-in">
                {t.status[status]}
              </span>
            </span>
          </div>

          <div className="relative">
            <div className="flex items-center justify-between">
              <span className="text-caption font-medium text-muted-foreground">{t.section}</span>
              <button
                type="button"
                aria-label={t.sectionMenu}
                aria-haspopup="menu"
                aria-expanded={phase === "menu"}
                disabled={!canUseMenu}
                onClick={() => setPhase(phase === "menu" ? "ready" : "menu")}
                className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground hover:bg-muted disabled:opacity-50 disabled:hover:bg-transparent"
              >
                <MoreHorizontal className="size-4" />
              </button>
            </div>
            {phase === "menu" && (
              <div
                role="menu"
                aria-label={t.sectionMenu}
                className="absolute top-7 right-0 z-10 w-56 rounded-lg border bg-popover p-1 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in"
              >
                <CoachMark quiet active ringClassName="rounded-md" className="flex w-full">
                  <button
                    type="button"
                    role="menuitem"
                    onClick={optOut}
                    className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-body hover:bg-accent"
                  >
                    <CircleSlash aria-hidden className="size-3.5 text-muted-foreground" />
                    {t.disable}
                  </button>
                </CoachMark>
              </div>
            )}

            <div className="mt-1 flex items-start gap-2 rounded-md px-0.5 py-1.5">
              {prMerged ? (
                <GitMerge aria-hidden className="mt-px size-3.5 shrink-0 text-violet-600 dark:text-violet-400" />
              ) : (
                <GitPullRequestArrow aria-hidden className="mt-px size-3.5 shrink-0 text-emerald-600 dark:text-emerald-400" />
              )}
              <div className="min-w-0 flex-1">
                <p className="truncate text-caption font-medium">{t.prTitle}</p>
                <div className="mt-1 flex items-center gap-2">
                  <p className="flex min-w-0 flex-1 items-center gap-1.5 text-micro text-muted-foreground tabular-nums">
                    <span>{t.ghNumber}</span>
                    <span aria-hidden>·</span>
                    {prMerged ? (
                      <span>{t.justNow}</span>
                    ) : (
                      <span>
                        <span className="text-emerald-600 dark:text-emerald-400">{t.diff.add}</span>{" "}
                        <span className="text-rose-600 dark:text-rose-400">{t.diff.del}</span>
                      </span>
                    )}
                  </p>
                  <span
                    className={cn(
                      "inline-flex shrink-0 items-center gap-1 rounded-full px-1.5 py-0.5 text-micro font-medium",
                      prMerged
                        ? "bg-violet-500/10 text-violet-700 dark:text-violet-300"
                        : "bg-emerald-500/10 text-emerald-700 dark:text-emerald-300",
                    )}
                  >
                    {prMerged ? <GitMerge aria-hidden className="size-3" /> : <CheckCircle2 aria-hidden className="size-3" />}
                    {prMerged ? t.merged : t.ready}
                  </span>
                </div>
              </div>
            </div>

            {autoLine && (
              <p className="mt-1 flex items-start gap-1.5 border-t pt-2 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in [&>svg]:mt-0.5 [&>svg]:size-3.5 [&>svg]:shrink-0">
                {autoLine}
              </p>
            )}
          </div>

          {(optedOut || status === "done") && (
            <ul className="mt-auto flex flex-col gap-1.5 border-t pt-2 text-caption text-muted-foreground">
              {optedOut && (
                <li className="flex min-w-0 items-start gap-2 motion-safe:animate-in motion-safe:fade-in">
                  <DemoAvatar name={t.you} />
                  <span className="min-w-0">
                    <span className="font-medium text-foreground">{t.you}</span> {t.optedOutActivity}
                  </span>
                </li>
              )}
              {status === "done" && (
                <li className="flex min-w-0 items-start gap-2 motion-safe:animate-in motion-safe:fade-in">
                  <span
                    aria-hidden
                    className="inline-flex size-5 shrink-0 items-center justify-center rounded-full bg-muted"
                  >
                    <GitMerge className="size-3" />
                  </span>
                  <span className="min-w-0">{t.mergedActivity}</span>
                </li>
              )}
            </ul>
          )}
        </section>
      </div>
    </DemoFrame>
  );
}
