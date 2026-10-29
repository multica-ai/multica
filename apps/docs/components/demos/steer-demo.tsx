"use client";

import { useEffect, useState } from "react";
import { CornerDownRight } from "@multica/ui/icons";
import {
  CoachMark,
  DemoAvatar,
  DemoComment,
  DemoFrame,
  DemoReceipt,
  DemoRun,
  DemoSendButton,
  DemoSteerMarker,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

const FEEDBACK_TOTAL = 86;

const copy = {
  en: {
    title: "OPS-12 · Turn this month's customer feedback into a weekly-meeting summary",
    agent: "Ops helper",
    you: "You",
    running: "Running",
    completed: "Completed",
    readTask: "Read the issue description and discussion",
    openSheet: `Open the feedback sheet (${FEEDBACK_TOTAL} entries)`,
    perItem: (n: number) => `Write a summary for each entry (${n}/${FEEDBACK_TOTAL})`,
    afterSteer: [
      "Group the feedback by theme: pricing, getting started, support response",
      "Write one takeaway per theme and pick 1–2 customer quotes",
      "Put it together on a single page",
    ],
    draft:
      "No need to summarize every entry. Group them by theme, give each theme one takeaway and 1–2 quotes, and keep it to one page.",
    placeholder: "Leave a comment...",
    chip: "Add to current run",
    send: "Send",
    waiting: "Waiting for Ops helper to read it",
    read: "Read by Ops helper · after step 3",
    steerLabel: "You steered",
    steerDetail: "Read",
    reply:
      "The summary is ready: three themes, each with one takeaway and two customer quotes, all on one page.",
    captions: {
      idle: "Ops helper is working on this issue.",
      working: "Ops helper is heading the wrong way: it is summarizing all 86 entries one by one.",
      ready: "The correction is already in the composer, and its recipient says \"Add to current run\". Click send.",
      sent: "Sent. Ops helper reads it as soon as the current step ends.",
      read: "It read the message and kept going in the new direction instead of starting over.",
      done: "Same run, new direction, original task finished. Use Replay to watch again.",
    },
  },
  zh: {
    title: "OPS-12 · 把本月客户反馈整理成周会汇报",
    agent: "运营助手",
    you: "你",
    running: "运行中",
    completed: "已完成",
    readTask: "读取任务描述和讨论",
    openSheet: `打开客户反馈表（共 ${FEEDBACK_TOTAL} 条）`,
    perItem: (n: number) => `逐条撰写反馈摘要（${n}/${FEEDBACK_TOTAL}）`,
    afterSteer: [
      "按主题归类：价格、上手难度、客服响应",
      "每类写一句结论，挑 1–2 条客户原话",
      "整理成一页汇报",
    ],
    draft: "不用逐条写摘要。按主题分几类，每类一句结论加 1–2 条原话，一页纸以内。",
    placeholder: "留下评论...",
    chip: "插话到当前运行",
    send: "发送",
    waiting: "等待 运营助手 读取",
    read: "运营助手 已读取 · 第 3 步后",
    steerLabel: "你 插话",
    steerDetail: "已读取",
    reply: "汇报已整理好：反馈分成 3 类，每类一句结论和 2 条客户原话，控制在一页内。",
    captions: {
      idle: "运营助手 正在执行这条任务。",
      working: "运营助手 的方向跑偏了：它在给 86 条反馈逐条写摘要。",
      ready: "评论框里已经写好了修正，收件人标签是\"插话到当前运行\"。点击发送。",
      sent: "插话已发出。运营助手 会在当前步骤结束后读到。",
      read: "它读到插话后没有从头开始，而是沿着新方向继续执行原任务。",
      done: "同一次运行、换了方向、完成了原任务。点右上角可以重新演示。",
    },
  },
  ja: {
    title: "OPS-12 · 今月の顧客フィードバックを週次ミーティング用にまとめる",
    agent: "運用アシスタント",
    you: "あなた",
    running: "実行中",
    completed: "完了",
    readTask: "タスクの説明とコメントを読む",
    openSheet: `フィードバック表を開く（全 ${FEEDBACK_TOTAL} 件）`,
    perItem: (n: number) => `1 件ずつ要約を書く（${n}/${FEEDBACK_TOTAL}）`,
    afterSteer: [
      "テーマ別に分ける：価格、使い始めやすさ、サポートの対応",
      "テーマごとに結論を 1 文書き、顧客の声を 1〜2 件選ぶ",
      "1 ページにまとめる",
    ],
    draft:
      "1 件ずつ要約しなくて大丈夫です。テーマ別に分けて、テーマごとに結論 1 文と顧客の声 1〜2 件、全体で 1 ページ以内にしてください。",
    placeholder: "コメントを残す...",
    chip: "現在の実行に追加",
    send: "送信",
    waiting: "運用アシスタント が読むのを待っています",
    read: "運用アシスタント が読みました · ステップ 3 の後",
    steerLabel: "あなた が追加",
    steerDetail: "既読",
    reply: "まとめができました。フィードバックを 3 つのテーマに分け、それぞれ結論 1 文と顧客の声 2 件を付けて 1 ページに収めています。",
    captions: {
      idle: "運用アシスタント がこのタスクを実行しています。",
      working: "運用アシスタント の進め方がずれています。86 件のフィードバックを 1 件ずつ要約しています。",
      ready: "修正の指示はもうコメント欄に書いてあり、送り先は「現在の実行に追加」になっています。送信をクリックします。",
      sent: "送信しました。運用アシスタント は今のステップが終わりしだい読みます。",
      read: "メッセージを読んだあと、最初からやり直さずに新しい方向で作業を続けています。",
      done: "同じ実行のまま方向を変え、元のタスクを終えました。「もう一度」で最初から見られます。",
    },
  },
  ko: {
    title: "OPS-12 · 이번 달 고객 피드백을 주간 회의 보고로 정리",
    agent: "운영 도우미",
    you: "나",
    running: "실행 중",
    completed: "완료됨",
    readTask: "태스크 설명과 댓글 읽기",
    openSheet: `고객 피드백 시트 열기(총 ${FEEDBACK_TOTAL}건)`,
    perItem: (n: number) => `피드백을 한 건씩 요약(${n}/${FEEDBACK_TOTAL})`,
    afterSteer: [
      "주제별로 분류: 가격, 시작 난이도, 고객 지원 응답",
      "주제마다 결론 한 문장을 쓰고 고객 발언 1~2개 고르기",
      "한 페이지로 정리",
    ],
    draft:
      "한 건씩 요약할 필요 없어요. 주제별로 나누고, 주제마다 결론 한 문장과 고객 발언 1~2개만 넣어 한 페이지 안으로 정리해 주세요.",
    placeholder: "댓글 남기기...",
    chip: "현재 실행에 추가",
    send: "보내기",
    waiting: "운영 도우미가 읽기를 기다리는 중",
    read: "운영 도우미가 읽음 · 3단계 후",
    steerLabel: "내가 추가",
    steerDetail: "읽음",
    reply: "보고를 정리했습니다. 피드백을 3개 주제로 나누고, 주제마다 결론 한 문장과 고객 발언 2개를 붙여 한 페이지에 담았습니다.",
    captions: {
      idle: "운영 도우미가 이 태스크를 실행하고 있습니다.",
      working: "운영 도우미가 엉뚱한 방향으로 가고 있습니다. 피드백 86건을 한 건씩 요약하는 중입니다.",
      ready: "수정 내용은 이미 댓글 입력란에 있고, 받는 사람 표시는 '현재 실행에 추가'입니다. 보내기를 클릭하세요.",
      sent: "보냈습니다. 운영 도우미는 현재 단계가 끝나는 대로 읽습니다.",
      read: "메시지를 읽은 뒤 처음부터 다시 하지 않고 새 방향으로 이어서 작업합니다.",
      done: "같은 실행에서 방향만 바꿔 원래 태스크를 끝냈습니다. '다시 보기'로 다시 볼 수 있습니다.",
    },
  },
  fr: {
    title: "OPS-12 · Résumer les retours clients du mois pour la réunion hebdo",
    agent: "Assistant ops",
    you: "Vous",
    running: "En cours",
    completed: "Terminée",
    readTask: "Lire la description et la discussion de la tâche",
    openSheet: `Ouvrir le tableau des retours (${FEEDBACK_TOTAL} entrées)`,
    perItem: (n: number) => `Résumer chaque entrée (${n}/${FEEDBACK_TOTAL})`,
    afterSteer: [
      "Regrouper les retours par thème : prix, prise en main, réactivité du support",
      "Écrire une conclusion par thème et choisir 1 à 2 citations de clients",
      "Tout rassembler sur une seule page",
    ],
    draft:
      "Inutile de résumer chaque entrée. Regroupez-les par thème, avec une conclusion et 1 à 2 citations par thème, sur une page au maximum.",
    placeholder: "Laisser un commentaire...",
    chip: "Ajouter à l'exécution en cours",
    send: "Envoyer",
    waiting: "En attente de lecture par Assistant ops",
    read: "Lu par Assistant ops · après l'étape 3",
    steerLabel: "Vous avez ajouté",
    steerDetail: "Lu",
    reply: "Le résumé est prêt : trois thèmes, chacun avec une conclusion et deux citations de clients, le tout sur une page.",
    captions: {
      idle: "Assistant ops travaille sur cette tâche.",
      working: "Assistant ops part dans la mauvaise direction : il résume les 86 entrées une par une.",
      ready:
        "La correction est déjà dans la zone de commentaire, et son destinataire indique « Ajouter à l'exécution en cours ». Cliquez sur envoyer.",
      sent: "Envoyé. Assistant ops le lira dès la fin de l'étape en cours.",
      read: "Il a lu le message et poursuit dans la nouvelle direction au lieu de tout recommencer.",
      done: "Même exécution, nouvelle direction, tâche d'origine terminée. Utilisez Rejouer pour revoir la démo.",
    },
  },
};

type Phase = "idle" | "working" | "ready" | "sent" | "read" | "done";

export function SteerDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [run, setRun] = useState(0);
  const [phase, setPhase] = useState<Phase>("idle");
  const [stepsShown, setStepsShown] = useState(0);
  const [count, setCount] = useState(1);
  const [newSteps, setNewSteps] = useState(0);

  // Opening: the agent works through its first steps, then starts the
  // item-by-item loop the reader is about to correct.
  useEffect(() => {
    if (!visible) return;
    clear();
    setPhase("working");
    setStepsShown(1);
    after(900, () => setStepsShown(2));
    after(1900, () => setStepsShown(3));
    after(2600, () => setPhase("ready"));
  }, [visible, run, after, clear]);

  // The wrong-direction loop keeps ticking until the steer is read.
  const looping = stepsShown >= 3 && (phase === "working" || phase === "ready" || phase === "sent");
  useEffect(() => {
    if (!looping) return;
    const id = window.setInterval(() => setCount((n) => Math.min(n + 1, 5)), 900);
    return () => window.clearInterval(id);
  }, [looping]);

  const send = () => {
    setPhase("sent");
    after(1800, () => setPhase("read"));
    for (let i = 1; i <= t.afterSteer.length; i++) after(1800 + i * 900, () => setNewSteps(i));
    after(1800 + t.afterSteer.length * 900 + 900, () => setPhase("done"));
  };

  const replay = () => {
    clear();
    setStepsShown(0);
    setCount(1);
    setNewSteps(0);
    setPhase("idle");
    setRun((r) => r + 1);
  };

  const sent = phase === "sent" || phase === "read" || phase === "done";
  const read = phase === "read" || phase === "done";
  // Only once the opening has finished: its last timer moves the phase to
  // "ready" and would otherwise overwrite a steer sent during the opening.
  const canSend = phase === "ready";
  const caption =
    phase === "working" && stepsShown >= 3 ? t.captions.working : t.captions[phase];

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={replay}>
      <div className="flex min-h-80 flex-col gap-3 p-4">
        <DemoRun
          agent={t.agent}
          status={phase === "done" ? t.completed : t.running}
          active={phase !== "done"}
        >
          {stepsShown >= 1 && <DemoStep state={stepsShown > 1 ? "done" : "running"}>{t.readTask}</DemoStep>}
          {stepsShown >= 2 && <DemoStep state={stepsShown > 2 ? "done" : "running"}>{t.openSheet}</DemoStep>}
          {stepsShown >= 3 && (
            <DemoStep state={read ? "done" : "running"}>{t.perItem(count)}</DemoStep>
          )}
          {read && <DemoSteerMarker label={t.steerLabel} detail={t.steerDetail} />}
          {t.afterSteer.slice(0, newSteps).map((step, i) => (
            <DemoStep key={step} state={i === newSteps - 1 && phase !== "done" ? "running" : "done"}>
              {step}
            </DemoStep>
          ))}
        </DemoRun>

        {sent && (
          <DemoComment
            author={t.you}
            footer={<DemoReceipt read={read}>{read ? t.read : t.waiting}</DemoReceipt>}
          >
            {t.draft}
          </DemoComment>
        )}

        {phase === "done" && (
          <DemoComment author={t.agent} agent>
            {t.reply}
          </DemoComment>
        )}

        <div className="mt-auto rounded-lg border bg-background p-2">
          {/* Static text, not an input: the demo is scripted around this exact correction. */}
          <div className="min-h-12 px-1 text-body">
            {sent || phase === "idle" ? <span className="text-muted-foreground">{t.placeholder}</span> : t.draft}
          </div>
          <div className="mt-1.5 flex items-center gap-2">
            {!sent && phase !== "idle" && (
              <span className="inline-flex h-6 min-w-0 items-center gap-1.5 rounded-md border border-brand/28 bg-brand/7 px-1.5 text-caption dark:border-brand/45 dark:bg-brand/12">
                <DemoAvatar name={t.agent} agent />
                <span className="truncate">{t.agent}</span>
                <CornerDownRight aria-hidden className="size-3 shrink-0 text-brand" />
                <span className="truncate font-medium">{t.chip}</span>
              </span>
            )}
            <span className="ml-auto" />
            <CoachMark active={phase === "ready" && canSend} side="left" ringClassName="rounded-full">
              <DemoSendButton label={t.send} disabled={!canSend} onClick={send} />
            </CoachMark>
          </div>
        </div>
      </div>
    </DemoFrame>
  );
}
