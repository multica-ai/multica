"use client";

import { useState } from "react";
import { Check, Plus, Search } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoComment,
  DemoFrame,
  DemoRun,
  DemoSendButton,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type SkillOption = { name: string; desc: string };

const SKILL_NAME = "weekly-report";

const copy = {
  en: {
    title: "Ops helper · Skills",
    assignedTitle: "Assigned to agent",
    assignedHint: "Turn off a skill to pause its use without removing it.",
    addAction: "Add skill",
    emptyTitle: "No skills assigned",
    workspaceBadge: "Workspace",
    dialogTitle: "Add skill",
    searchPlaceholder: "Search skills",
    skills: [
      { name: SKILL_NAME, desc: "Weekly reports in three parts: Done this week, Next week, Risks. At most three points each." },
      { name: "event-recap", desc: "Event recap template with sign-ups, turnout and follow-ups." },
      { name: "support-reply", desc: "Tone and structure for replying to customer tickets." },
    ] as SkillOption[],
    cancel: "Cancel",
    confirmDefault: "Add",
    confirmOne: "Add 1 skill",
    issueTitle: "OPS-17 · Write this week's ops report",
    agent: "Ops helper",
    you: "You",
    draft: "write up this week's ops report.",
    send: "Send",
    running: "Running",
    completed: "Completed",
    steps: [
      "Read this comment and the issue description",
      `Skill · ${SKILL_NAME}`,
      "Collect this week's event, support and content numbers",
      `Write the report in the ${SKILL_NAME} format`,
    ],
    reply: [
      { head: "Done this week", body: "Ran the members' day event, 326 sign-ups; first support response down to 4 minutes." },
      { head: "Next week", body: "Publish the event recap; sort October's customer feedback." },
      { head: "Risks", body: "8% of the event budget is left, so next week's materials may need more." },
    ],
    coachPick: `Pick ${SKILL_NAME}`,
    captions: {
      idle: "Ops helper has no skills yet.",
      start: "Give Ops helper a weekly-report method it can reuse: click Add skill.",
      pick: "These are the workspace's skills. Pick weekly-report.",
      confirm: "Selected. Click Add 1 skill.",
      added: "Assigned. Runs from now on can use it. Now ask Ops helper for this week's report: click send.",
      running: "In the run, the agent loads the skill before writing; that call shows up as a step.",
      done: "The report follows the skill's three parts. Edits to the skill apply from the next run. Use Replay to watch again.",
    },
  },
  zh: {
    title: "运营助手 · Skills",
    assignedTitle: "分配给智能体",
    assignedHint: "关闭后暂停使用，仍保留分配。",
    addAction: "添加 skill",
    emptyTitle: "尚未分配 skill",
    workspaceBadge: "工作区",
    dialogTitle: "添加 skill",
    searchPlaceholder: "搜索 skill",
    skills: [
      { name: SKILL_NAME, desc: "周报按\"本周完成 / 下周计划 / 风险\"三段写，每段最多三条。" },
      { name: "event-recap", desc: "活动复盘模板：报名、到场和后续跟进。" },
      { name: "support-reply", desc: "回复客户工单的语气和结构。" },
    ] as SkillOption[],
    cancel: "取消",
    confirmDefault: "添加",
    confirmOne: "添加 1 个 skill",
    issueTitle: "OPS-17 · 写本周运营周报",
    agent: "运营助手",
    you: "你",
    draft: "写一下本周的运营周报。",
    send: "发送",
    running: "运行中",
    completed: "已完成",
    steps: [
      "读取这条评论和任务描述",
      `Skill · ${SKILL_NAME}`,
      "汇总本周的活动、客服和内容数据",
      `按 ${SKILL_NAME} 的格式写周报`,
    ],
    reply: [
      { head: "本周完成", body: "会员日活动上线，报名 326 人；客服首次响应缩短到 4 分钟。" },
      { head: "下周计划", body: "发布活动复盘；整理 10 月客户反馈。" },
      { head: "风险", body: "活动预算只剩 8%，下周物料可能要追加。" },
    ],
    coachPick: `选择 ${SKILL_NAME}`,
    captions: {
      idle: "运营助手 还没有分配任何 skill。",
      start: "给运营助手一套可以复用的周报写法：点击\"添加 skill\"。",
      pick: "这里列出工作区里的 skill。选择 weekly-report。",
      confirm: "已选中。点击\"添加 1 个 skill\"。",
      added: "分配完成，之后的运行都能用它。到任务里请运营助手写本周周报：点击发送。",
      running: "运行中，智能体先加载 skill 再动笔，这次调用会作为一个步骤出现在运行记录里。",
      done: "周报按 skill 规定的三段交付。之后修改 skill，从下一次运行开始生效。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "運用アシスタント · スキル",
    assignedTitle: "エージェントに割り当て済み",
    assignedHint: "オフにすると割り当てを残したまま使用を停止できます。",
    addAction: "スキルを追加",
    emptyTitle: "割り当てられたスキルがありません",
    workspaceBadge: "ワークスペース",
    dialogTitle: "スキルを追加",
    searchPlaceholder: "スキルを検索",
    skills: [
      { name: SKILL_NAME, desc: "週報を「今週やったこと / 来週の予定 / リスク」の 3 部で書きます。各部は最大 3 項目。" },
      { name: "event-recap", desc: "イベント振り返りのテンプレート：申込数、参加数、フォローアップ。" },
      { name: "support-reply", desc: "顧客チケットに返信するときのトーンと構成。" },
    ] as SkillOption[],
    cancel: "キャンセル",
    confirmDefault: "追加",
    confirmOne: "スキル 1 件を追加",
    issueTitle: "OPS-17 · 今週の運用週報を書く",
    agent: "運用アシスタント",
    you: "あなた",
    draft: "今週の運用週報をまとめてください。",
    send: "送信",
    running: "実行中",
    completed: "完了",
    steps: [
      "このコメントとタスクの説明を読む",
      `Skill · ${SKILL_NAME}`,
      "今週のイベント、サポート、コンテンツの数字を集める",
      `${SKILL_NAME} の形式で週報を書く`,
    ],
    reply: [
      { head: "今週やったこと", body: "会員デーのイベントを実施し、申込は 326 人。サポートの初回応答は 4 分まで短縮しました。" },
      { head: "来週の予定", body: "イベントの振り返りを公開し、10 月の顧客フィードバックを整理します。" },
      { head: "リスク", body: "イベント予算の残りが 8% なので、来週の資材は追加が必要になるかもしれません。" },
    ],
    coachPick: `${SKILL_NAME} を選ぶ`,
    captions: {
      idle: "運用アシスタントにはまだスキルがありません。",
      start: "運用アシスタントに、使い回せる週報の書き方を渡します。「スキルを追加」をクリックします。",
      pick: "ワークスペースにあるスキルが並んでいます。weekly-report を選びます。",
      confirm: "選択しました。「スキル 1 件を追加」をクリックします。",
      added: "割り当てが済み、これ以降の実行で使えます。タスクで運用アシスタントに今週の週報を頼みます。送信をクリックします。",
      running: "実行の中で、エージェントは書き始める前にスキルを読み込みます。この呼び出しは 1 つのステップとして記録に残ります。",
      done: "週報はスキルが決めた 3 部構成で届きました。スキルを編集すると、次の実行から反映されます。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "운영 도우미 · 스킬",
    assignedTitle: "에이전트에 할당됨",
    assignedHint: "끄면 할당은 유지한 채 사용을 중지합니다.",
    addAction: "스킬 추가",
    emptyTitle: "할당된 스킬이 없습니다",
    workspaceBadge: "워크스페이스",
    dialogTitle: "스킬 추가",
    searchPlaceholder: "스킬 검색",
    skills: [
      { name: SKILL_NAME, desc: "주간 보고를 '이번 주 완료 / 다음 주 계획 / 리스크' 세 부분으로 씁니다. 부분마다 최대 세 항목." },
      { name: "event-recap", desc: "행사 회고 템플릿: 신청, 참석, 후속 조치." },
      { name: "support-reply", desc: "고객 티켓에 답할 때의 어조와 구성." },
    ] as SkillOption[],
    cancel: "취소",
    confirmDefault: "추가",
    confirmOne: "스킬 1개 추가",
    issueTitle: "OPS-17 · 이번 주 운영 주간 보고 작성",
    agent: "운영 도우미",
    you: "나",
    draft: "이번 주 운영 주간 보고를 정리해 주세요.",
    send: "보내기",
    running: "실행 중",
    completed: "완료됨",
    steps: [
      "이 댓글과 태스크 설명 읽기",
      `Skill · ${SKILL_NAME}`,
      "이번 주 행사, 고객 지원, 콘텐츠 수치 모으기",
      `${SKILL_NAME} 형식으로 주간 보고 작성`,
    ],
    reply: [
      { head: "이번 주 완료", body: "회원의 날 행사를 진행했고 신청은 326명입니다. 고객 지원 첫 응답 시간은 4분으로 줄었습니다." },
      { head: "다음 주 계획", body: "행사 회고를 공개하고 10월 고객 피드백을 정리합니다." },
      { head: "리스크", body: "행사 예산이 8%만 남아 다음 주 물품은 추가 예산이 필요할 수 있습니다." },
    ],
    coachPick: `${SKILL_NAME} 선택`,
    captions: {
      idle: "운영 도우미에게는 아직 스킬이 없습니다.",
      start: "운영 도우미에게 다시 쓸 수 있는 주간 보고 작성법을 줍니다. 스킬 추가를 클릭하세요.",
      pick: "워크스페이스에 있는 스킬 목록입니다. weekly-report를 고르세요.",
      confirm: "선택했습니다. 스킬 1개 추가를 클릭하세요.",
      added: "할당이 끝나 이후 실행부터 쓸 수 있습니다. 이제 태스크에서 운영 도우미에게 이번 주 보고를 부탁합니다. 보내기를 클릭하세요.",
      running: "실행 중에 에이전트는 글을 쓰기 전에 스킬부터 불러옵니다. 이 호출은 하나의 단계로 기록됩니다.",
      done: "보고는 스킬이 정한 세 부분으로 나왔습니다. 스킬을 수정하면 다음 실행부터 반영됩니다. 다시 보기를 누르면 처음부터 볼 수 있습니다.",
    },
  },
  fr: {
    title: "Assistant ops · Skills",
    assignedTitle: "Assignés à l'agent",
    assignedHint: "Désactivez un skill pour suspendre son utilisation sans le retirer.",
    addAction: "Ajouter un skill",
    emptyTitle: "Aucun skill assigné",
    workspaceBadge: "Espace de travail",
    dialogTitle: "Ajouter un skill",
    searchPlaceholder: "Rechercher des skills",
    skills: [
      { name: SKILL_NAME, desc: "Rapport hebdomadaire en trois parties : Fait cette semaine, Semaine prochaine, Risques. Trois points maximum par partie." },
      { name: "event-recap", desc: "Modèle de bilan d'événement : inscriptions, participation et suites à donner." },
      { name: "support-reply", desc: "Ton et structure pour répondre aux tickets clients." },
    ] as SkillOption[],
    cancel: "Annuler",
    confirmDefault: "Ajouter",
    confirmOne: "Ajouter 1 skill",
    issueTitle: "OPS-17 · Rédiger le rapport ops de la semaine",
    agent: "Assistant ops",
    you: "Vous",
    draft: "rédige le rapport ops de cette semaine.",
    send: "Envoyer",
    running: "En cours",
    completed: "Terminée",
    steps: [
      "Lire ce commentaire et la description de la tâche",
      `Skill · ${SKILL_NAME}`,
      "Rassembler les chiffres de la semaine : événements, support, contenu",
      `Rédiger le rapport au format ${SKILL_NAME}`,
    ],
    reply: [
      { head: "Fait cette semaine", body: "Journée membres organisée, 326 inscriptions ; premier délai de réponse du support ramené à 4 minutes." },
      { head: "Semaine prochaine", body: "Publier le bilan de l'événement ; trier les retours clients d'octobre." },
      { head: "Risques", body: "Il reste 8 % du budget événementiel ; le matériel de la semaine prochaine pourrait demander une rallonge." },
    ],
    coachPick: `Choisir ${SKILL_NAME}`,
    captions: {
      idle: "Assistant ops n'a encore aucun skill.",
      start: "Donnez à Assistant ops une méthode de rapport hebdomadaire réutilisable : cliquez sur Ajouter un skill.",
      pick: "Voici les skills de l'espace de travail. Choisissez weekly-report.",
      confirm: "Sélectionné. Cliquez sur Ajouter 1 skill.",
      added: "C'est assigné : les exécutions suivantes peuvent l'utiliser. Demandez maintenant le rapport de la semaine à Assistant ops : cliquez sur envoyer.",
      running: "Pendant l'exécution, l'agent charge le skill avant de rédiger ; cet appel apparaît comme une étape.",
      done: "Le rapport suit les trois parties du skill. Une modification du skill s'applique à partir de l'exécution suivante. Cliquez sur Rejouer pour revoir la démo.",
    },
  },
};

type Phase = "idle" | "start" | "pick" | "confirm" | "added" | "running" | "done";

// One scripted path: attach the skill on the agent's Skills tab, then watch a
// run use it. The composer and the dialog's search are fixed text.
export function SkillDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [phase, setPhase] = useState<Exclude<Phase, "idle">>("start");
  const [stepsShown, setStepsShown] = useState(0);

  const shownPhase: Phase = visible ? phase : "idle";
  const attached = phase === "added" || phase === "running" || phase === "done";
  const sent = phase === "running" || phase === "done";
  const dialogOpen = phase === "pick" || phase === "confirm";
  const skill = t.skills[0]!;

  const send = () => {
    setPhase("running");
    t.steps.forEach((_, i) => after(500 + i * 1000, () => setStepsShown(i + 1)));
    after(500 + t.steps.length * 1000 + 600, () => setPhase("done"));
  };

  const replay = () => {
    clear();
    setPhase("start");
    setStepsShown(0);
  };

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={t.captions[shownPhase]} onReplay={replay}>
      <div className="relative flex min-h-80 flex-col gap-4 p-4">
        <section className="rounded-lg border bg-background">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2 border-b px-3 py-2">
            <div className="min-w-0 flex-1">
              <div className="text-label font-medium">{t.assignedTitle}</div>
              <div className="text-caption text-muted-foreground">{t.assignedHint}</div>
            </div>
            <CoachMark active={shownPhase === "start"} side="left" ringClassName="rounded-md">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={shownPhase !== "start"}
                onClick={() => setPhase("pick")}
              >
                <Plus aria-hidden className="size-3.5" />
                {t.addAction}
              </Button>
            </CoachMark>
          </div>
          {attached ? (
            <div className="flex items-start gap-3 px-3 py-2.5 motion-safe:animate-in motion-safe:fade-in">
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-mono text-label font-medium">{skill.name}</span>
                  <span className="rounded-xs bg-muted px-1 text-micro text-muted-foreground">{t.workspaceBadge}</span>
                </div>
                <div className="mt-0.5 text-caption text-muted-foreground">{skill.desc}</div>
              </div>
              <span aria-hidden className="mt-0.5 inline-flex h-4 w-7 shrink-0 items-center rounded-full bg-foreground p-0.5">
                <span className="ml-auto size-3 rounded-full bg-background" />
              </span>
            </div>
          ) : (
            <div className="px-3 py-5 text-center text-caption text-muted-foreground">{t.emptyTitle}</div>
          )}
        </section>

        {attached && (
          <section className="flex flex-col gap-3 motion-safe:animate-in motion-safe:fade-in">
            <div className="text-label font-medium">{t.issueTitle}</div>

            {sent && (
              <>
                <DemoComment author={t.you}>
                  <span className="font-medium text-brand">@{t.agent}</span> {t.draft}
                </DemoComment>
                <DemoRun agent={t.agent} status={phase === "done" ? t.completed : t.running} active={phase !== "done"}>
                  {t.steps.slice(0, stepsShown).map((step, i) => (
                    <DemoStep key={step} state={i === stepsShown - 1 && phase !== "done" ? "running" : "done"}>
                      {i === 1 ? <span className="font-mono text-foreground">{step}</span> : step}
                    </DemoStep>
                  ))}
                </DemoRun>
              </>
            )}

            {phase === "done" && (
              <DemoComment author={t.agent} agent>
                <div className="flex flex-col gap-1">
                  {t.reply.map((part) => (
                    <p key={part.head}>
                      <span className="font-medium">{part.head}</span>
                      <span className="text-muted-foreground"> · </span>
                      {part.body}
                    </p>
                  ))}
                </div>
              </DemoComment>
            )}

            {!sent && (
              <div className="rounded-lg border bg-background p-2">
                <div className="min-h-10 px-1 text-body">
                  <span className="font-medium text-brand">@{t.agent}</span> {t.draft}
                </div>
                <div className="mt-1 flex justify-end">
                  <CoachMark active={phase === "added"} side="left" ringClassName="rounded-full">
                    <DemoSendButton label={t.send} disabled={phase !== "added"} onClick={send} />
                  </CoachMark>
                </div>
              </div>
            )}
          </section>
        )}

        {dialogOpen && (
          <div className="absolute inset-0 z-20 flex items-start justify-center bg-background/60 p-4 backdrop-blur-[2px] motion-safe:animate-in motion-safe:fade-in">
            <div
              role="dialog"
              aria-modal="false"
              aria-labelledby="skill-demo-dialog-title"
              className="w-full max-w-sm rounded-xl border bg-popover text-popover-foreground shadow-lg motion-safe:animate-in motion-safe:zoom-in-95"
            >
              <div id="skill-demo-dialog-title" className="px-4 pt-4 text-body font-medium">
                {t.dialogTitle}
              </div>
              <div className="px-4 pt-3">
                <div className="flex h-8 items-center gap-2 rounded-md border px-2 text-body text-muted-foreground">
                  <Search aria-hidden className="size-3.5" />
                  {t.searchPlaceholder}
                </div>
              </div>
              <div className="flex flex-col gap-0.5 p-2">
                {t.skills.map((option, i) => {
                  const checked = i === 0 && phase === "confirm";
                  const enabled = i === 0 && phase === "pick";
                  return (
                    <CoachMark quiet key={option.name} active={enabled} ringClassName="rounded-md" className="flex w-full">
                      <button
                        type="button"
                        role="checkbox"
                        aria-checked={checked}
                        disabled={!enabled}
                        onClick={() => setPhase("confirm")}
                        className={cn(
                          "flex w-full items-start gap-2.5 rounded-md px-2 py-1.5 text-left hover:bg-accent disabled:hover:bg-transparent",
                          checked && "bg-accent/60",
                        )}
                      >
                        <span
                          aria-hidden
                          className={cn(
                            "mt-0.5 flex size-4 shrink-0 items-center justify-center rounded-sm border",
                            checked ? "border-foreground bg-foreground text-background" : "border-muted-foreground/50",
                          )}
                        >
                          {checked && <Check className="size-3" />}
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="flex flex-wrap items-baseline gap-x-2">
                            <span className="font-mono text-label font-medium">{option.name}</span>
                            {enabled && (
                              <span aria-hidden className="text-caption font-medium text-brand">
                                {t.coachPick}
                              </span>
                            )}
                          </span>
                          <span className="block truncate text-caption text-muted-foreground">{option.desc}</span>
                        </span>
                      </button>
                    </CoachMark>
                  );
                })}
              </div>
              <div className="flex justify-end gap-2 rounded-b-xl border-t bg-muted/40 px-4 py-3">
                <Button type="button" variant="ghost" size="sm" disabled>
                  {t.cancel}
                </Button>
                <CoachMark active={phase === "confirm"} side="bottom" align="end" ringClassName="rounded-lg">
                  <Button type="button" size="sm" disabled={phase !== "confirm"} onClick={() => setPhase("added")}>
                    {phase === "confirm" ? t.confirmOne : t.confirmDefault}
                  </Button>
                </CoachMark>
              </div>
            </div>
          </div>
        )}
      </div>
    </DemoFrame>
  );
}
