"use client";

import { useEffect, useState, type ReactNode } from "react";
import { Bell, ChevronDown, ChevronRight, Clock3, FastForward, Info, Plus, RefreshCw } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  CoachMark,
  DemoAvatar,
  DemoComment,
  DemoFrame,
  DemoRun,
  DemoStep,
  useDemoCopy,
  useDemoTimers,
  useStartWhenVisible,
} from "./demo-kit";

type Choice = "at" | "recurring";

const copy = {
  en: {
    title: "OPS-56 · Confirm supplier quotes for trade-show materials",
    agent: "Ops helper",
    running: "Running",
    completed: "Completed",
    opening:
      "I've sent quote requests to 3 suppliers and asked them to reply by end of day. I can't compare prices until the quotes are in, so I'm stopping here for now.",
    section: "Wakeups",
    newWakeup: "New wakeup",
    formTitle: "New wakeup",
    when: "When",
    wake: "Wake",
    chooseCondition: "Choose a condition",
    groupTime: "Time",
    condAt: "At a time",
    condAtHint: "In 10 min, tomorrow 09:00",
    condRecurring: "Check regularly",
    condRecurringHint: "Hourly or daily, with an end date",
    atPresets: ["In 10 minutes", "In 1 hour", "Tomorrow 09:00", "Pick a time"],
    daily: "Every day 09:00",
    until: "Until",
    untilValue: "10/27/2026",
    instructionTitle: "What to do when woken",
    instruction:
      "Check the quote emails to see whether all 3 suppliers have replied. If they have, put the quotes in a comparison table and recommend one.",
    onBehalf: "Runs act on your behalf",
    cancel: "Cancel",
    create: "Create",
    rule: { at: "Wake at Oct 21, 09:00 AM", recurring: "At 09:00 · Every day" } as Record<Choice, string>,
    summaryOnce: "Wake Ops helper · Trigger once",
    summaryFired: "Wake Ops helper · Trigger once · Triggered",
    summaryNext: (date: number) =>
      `Wake Ops helper · Next: Oct ${date}, 09:00 AM · until Oct 27`,
    summaryOff: "Wake Ops helper · Turned off",
    thisRun: (done: boolean) => `This execution: ${done ? "Run succeeded" : "Running"}`,
    lastRun: "Last execution: Run succeeded",
    history: "Wakeup history 1",
    chip: (date: number) => `Oct ${date}, 09:00 AM · wakes Ops helper`,
    source: { at: "Wakeup · Wake at Oct 21, 09:00 AM", recurring: "Wakeup · At 09:00 · Every day" } as Record<Choice, string>,
    steps: {
      read: "Read the issue, comments and wakeup instruction",
      found: "Check the quote emails: all 3 have replied",
      none: "Check the quote emails: no replies yet",
      compare: "Build the quote comparison table",
      checkin: "Nothing needs a reply: end with a silent check",
      disable: "Quotes are in: turn off this regular check",
    },
    checkin: "silent check: None of the 3 suppliers has sent a quote yet; checking again tomorrow",
    reply:
      "All 3 quotes are in: A is $260 a set with delivery in 10 days, B $240 in 14 days, C $275 in 7 days. The show is 3 weeks away, so I recommend B. The comparison table is attached to the issue.",
    fastForward: (date: number) => `Skip ahead to Oct ${date}, 09:00 AM`,
    captions: {
      idle: "Ops helper has asked 3 suppliers for quotes and is waiting for their replies.",
      ready: "The quotes won't arrive before tomorrow. Click + in the Wakeups section so Ops helper comes back to this issue later.",
      form: "The issue's assignee, Ops helper, is already selected, and the instruction is filled in. Open the condition menu.",
      menu: "Either time condition works here: \"At a time\" wakes Ops helper once; \"Check regularly\" wakes it every day until the end date.",
      preset: "The time defaults to one hour from now. Change it to tomorrow 09:00.",
      presetMenu: "Pick \"Tomorrow 09:00\".",
      filled: {
        at: "Ops helper will be woken once, tomorrow at 09:00. Click Create.",
        recurring: "Ops helper will be woken every day at 09:00 until Oct 27. Click Create.",
      } as Record<Choice, string>,
      scheduled:
        "The rule is saved on the issue, and the header shows what the issue is waiting for. No run is active in the meantime. Skip ahead to the wakeup time.",
      running: "It's 09:00: Ops helper starts a new run on this issue with the wakeup's instruction.",
      checked:
        "No replies yet. This run posted no comment; it left a silent check in the timeline instead. Skip ahead another day.",
      running2: "The next day's check starts.",
      done: {
        at: "Ops helper found the quotes and finished the comparison. A one-time wakeup ends after it fires and moves to Wakeup history. Use Replay to try the other condition.",
        recurring:
          "The quotes arrived. Ops helper posted the comparison and turned off the regular check. Use Replay to try the other condition.",
      } as Record<Choice, string>,
    },
  },
  zh: {
    title: "OPS-56 · 确认展会物料的供应商报价",
    agent: "运营助手",
    running: "运行中",
    completed: "已完成",
    opening: "已向 3 家供应商发出询价，请他们今天下班前回复。报价到齐前没法比价，先停在这里。",
    section: "唤醒",
    newWakeup: "新建唤醒",
    formTitle: "新建唤醒",
    when: "当",
    wake: "唤醒",
    chooseCondition: "选择条件",
    groupTime: "时间",
    condAt: "在某个时间",
    condAtHint: "10 分钟后、明天 09:00",
    condRecurring: "定期检查",
    condRecurringHint: "每小时、每天，需设截止日",
    atPresets: ["10 分钟后", "1 小时后", "明天 09:00", "自定义时间"],
    daily: "每天 09:00",
    until: "截止",
    untilValue: "2026/10/27",
    instructionTitle: "唤醒后要做什么",
    instruction: "查看询价邮件，看 3 家供应商是否都回了报价。回齐了就整理成对比表，给出建议。",
    onBehalf: "唤醒后的运行将代表你执行",
    cancel: "取消",
    create: "创建",
    rule: { at: "10月21日 09:00 唤醒", recurring: "09:00 · 每天" } as Record<Choice, string>,
    summaryOnce: "唤醒 运营助手 · 仅触发一次",
    summaryFired: "唤醒 运营助手 · 仅触发一次 · 已触发",
    summaryNext: (date: number) => `唤醒 运营助手 · 下次 10月${date}日 09:00 · 至 10月27日`,
    summaryOff: "唤醒 运营助手 · 已关闭",
    thisRun: (done: boolean) => `本次执行：${done ? "运行成功" : "运行中"}`,
    lastRun: "最近一次：运行成功",
    history: "历史唤醒 1",
    chip: (date: number) => `10月${date}日 09:00 唤醒 运营助手`,
    source: { at: "由唤醒触发 · 10月21日 09:00 唤醒", recurring: "由唤醒触发 · 09:00 · 每天" } as Record<Choice, string>,
    steps: {
      read: "读取任务、评论和唤醒指令",
      found: "查看询价邮件：3 家都已回复",
      none: "查看询价邮件：还没有回复",
      compare: "整理报价对比表",
      checkin: "没有需要回复的内容，以静默检查结束",
      disable: "报价已齐，关闭这条定期检查",
    },
    checkin: "静默检查：3 家供应商都还没回复报价，明天再看",
    reply:
      "3 家报价都到了：A 每套 2,600 元，10 天交货；B 每套 2,400 元，14 天交货；C 每套 2,750 元，7 天交货。离展会还有 3 周，建议选 B。对比表已附在任务里。",
    fastForward: (date: number) => `快进到 10月${date}日 09:00`,
    captions: {
      idle: "运营助手 已向 3 家供应商询价，正在等回复。",
      ready: "报价明天之前到不了。点唤醒区域的 +，让 运营助手 之后再回到这条任务。",
      form: "唤醒对象已预选为任务负责人 运营助手，指令也写好了。打开条件菜单。",
      menu: "这里两种时间条件都能用：\"在某个时间\"只唤醒一次，\"定期检查\"每天唤醒一次，直到截止日。",
      preset: "时间默认是 1 小时后，改成明天 09:00。",
      presetMenu: "选择\"明天 09:00\"。",
      filled: {
        at: "明天 09:00 唤醒 运营助手 一次。点击创建。",
        recurring: "每天 09:00 唤醒 运营助手，到 10月27日 截止。点击创建。",
      } as Record<Choice, string>,
      scheduled: "规则存在任务上，标题旁显示这条任务在等什么。这期间没有运行在跑。快进到唤醒时间。",
      running: "到 09:00 了：运营助手 带着唤醒指令，在这条任务上开始一次新的运行。",
      checked: "还没有回复。这次运行没有发评论，只在时间线留下一条静默检查。再快进一天。",
      running2: "第二天的检查开始了。",
      done: {
        at: "运营助手 读到了报价，把比价做完。单次唤醒触发后就结束，移到了历史唤醒。点右上角重新演示，可以试另一种条件。",
        recurring: "报价到了。运营助手 发出比价结果，并关掉了这条定期检查。点右上角重新演示，可以试另一种条件。",
      } as Record<Choice, string>,
    },
  },
  ja: {
    title: "OPS-56 · 展示会資材の仕入れ先見積もりを確認する",
    agent: "運用アシスタント",
    running: "実行中",
    completed: "完了",
    opening:
      "仕入れ先 3 社に見積もりを依頼し、今日の終業までに返信をお願いしました。見積もりがそろうまで比較できないので、いったんここで止めます。",
    section: "ウェイクアップ",
    newWakeup: "新しいウェイクアップ",
    formTitle: "新しいウェイクアップ",
    when: "条件",
    wake: "起こす",
    chooseCondition: "条件を選択",
    groupTime: "時間",
    condAt: "指定した時刻",
    condAtHint: "10 分後、明日 09:00",
    condRecurring: "定期チェック",
    condRecurringHint: "毎時・毎日、終了日を設定",
    atPresets: ["10 分後", "1 時間後", "明日 09:00", "時刻を指定"],
    daily: "毎日 09:00",
    until: "終了日",
    untilValue: "2026/10/27",
    instructionTitle: "起動後に行うこと",
    instruction:
      "見積もりメールを確認し、3 社すべてから返信が来ているか見てください。そろっていれば比較表にまとめ、おすすめを示してください。",
    onBehalf: "起動された実行はあなたの代理として動きます",
    cancel: "キャンセル",
    create: "作成",
    rule: { at: "10月21日 09:00 に起動", recurring: "09:00 · 毎日" } as Record<Choice, string>,
    summaryOnce: "運用アシスタント を起動 · 一度だけ起動",
    summaryFired: "運用アシスタント を起動 · 一度だけ起動 · 起動済み",
    summaryNext: (date: number) =>
      `運用アシスタント を起動 · 次回：10月${date}日 09:00 · 10月27日 まで`,
    summaryOff: "運用アシスタント を起動 · 無効",
    thisRun: (done: boolean) => `今回の実行：${done ? "実行成功" : "実行中"}`,
    lastRun: "前回の実行：実行成功",
    history: "ウェイクアップ履歴 1",
    chip: (date: number) => `10月${date}日 09:00 に 運用アシスタント を起こす`,
    source: { at: "起動 · 10月21日 09:00 に起動", recurring: "起動 · 09:00 · 毎日" } as Record<Choice, string>,
    steps: {
      read: "タスク、コメント、ウェイクアップの指示を読む",
      found: "見積もりメールを確認：3 社とも返信あり",
      none: "見積もりメールを確認：まだ返信なし",
      compare: "見積もりの比較表を作る",
      checkin: "返信が必要な内容はないので、サイレントチェックで終える",
      disable: "見積もりがそろったので、この定期チェックをオフにする",
    },
    checkin: "サイレントチェック：3 社ともまだ見積もりの返信なし。明日また確認",
    reply:
      "3 社の見積もりがそろいました。A は 1 セット 52,000 円で納期 10 日、B は 48,000 円で 14 日、C は 55,000 円で 7 日です。展示会まで 3 週間あるので B をおすすめします。比較表はタスクに添付しました。",
    fastForward: (date: number) => `10月${date}日 09:00 まで早送り`,
    captions: {
      idle: "運用アシスタント は仕入れ先 3 社に見積もりを依頼し、返信を待っています。",
      ready: "見積もりは明日まで届きません。ウェイクアップ欄の + をクリックして、運用アシスタント が後でこのタスクに戻るようにします。",
      form: "起こす相手にはタスクの担当者 運用アシスタント が選ばれていて、指示も入力済みです。条件メニューを開きます。",
      menu: "ここではどちらの時間条件も使えます。「指定した時刻」は一度だけ、「定期チェック」は終了日まで毎日起こします。",
      preset: "時刻の既定は 1 時間後です。明日 09:00 に変えます。",
      presetMenu: "「明日 09:00」を選びます。",
      filled: {
        at: "明日 09:00 に 運用アシスタント を一度だけ起こします。作成をクリックします。",
        recurring: "10月27日 まで毎日 09:00 に 運用アシスタント を起こします。作成をクリックします。",
      } as Record<Choice, string>,
      scheduled:
        "ルールはタスクに保存され、ヘッダーにはこのタスクが何を待っているかが表示されます。その間に動いている実行はありません。起動時刻まで早送りします。",
      running: "09:00 になりました。運用アシスタント がウェイクアップの指示を持って、このタスクで新しい実行を始めます。",
      checked:
        "まだ返信はありません。この実行はコメントを投稿せず、タイムラインにサイレントチェックを 1 件残しました。もう 1 日早送りします。",
      running2: "翌日のチェックが始まりました。",
      done: {
        at: "運用アシスタント は見積もりを読み、比較を仕上げました。一度だけのウェイクアップは起動後に終わり、履歴に移ります。「もう一度」でもう一方の条件も試せます。",
        recurring:
          "見積もりが届きました。運用アシスタント は比較結果を投稿し、定期チェックをオフにしました。「もう一度」でもう一方の条件も試せます。",
      } as Record<Choice, string>,
    },
  },
  ko: {
    title: "OPS-56 · 전시회 물품 공급업체 견적 확인",
    agent: "운영 도우미",
    running: "실행 중",
    completed: "완료됨",
    opening:
      "공급업체 3곳에 견적을 요청하고 오늘 퇴근 전까지 회신해 달라고 했습니다. 견적이 다 모여야 비교할 수 있어서 일단 여기서 멈춥니다.",
    section: "깨우기",
    newWakeup: "새 웨이크업",
    formTitle: "새 웨이크업",
    when: "조건",
    wake: "깨울 대상",
    chooseCondition: "조건 선택",
    groupTime: "시간",
    condAt: "특정 시각",
    condAtHint: "10분 후, 내일 09:00",
    condRecurring: "정기 확인",
    condRecurringHint: "매시간·매일, 종료일 필요",
    atPresets: ["10분 후", "1시간 후", "내일 09:00", "시간 지정"],
    daily: "매일 09:00",
    until: "종료일",
    untilValue: "2026. 10. 27.",
    instructionTitle: "깨운 후 수행할 일",
    instruction: "견적 메일을 확인해 3곳 모두 회신했는지 봐 주세요. 다 모였으면 비교표로 정리하고 추천안을 제시해 주세요.",
    onBehalf: "시작된 실행은 사용자를 대신해 동작합니다",
    cancel: "취소",
    create: "만들기",
    rule: { at: "10월 21일 오전 09:00에 깨우기", recurring: "09:00 · 매일" } as Record<Choice, string>,
    summaryOnce: "운영 도우미 깨우기 · 한 번만 트리거",
    summaryFired: "운영 도우미 깨우기 · 한 번만 트리거 · 트리거됨",
    summaryNext: (date: number) =>
      `운영 도우미 깨우기 · 다음: 10월 ${date}일 오전 09:00 · 10월 27일까지`,
    summaryOff: "운영 도우미 깨우기 · 꺼짐",
    thisRun: (done: boolean) => `이번 실행: ${done ? "실행 성공" : "실행 중"}`,
    lastRun: "최근 실행: 실행 성공",
    history: "깨우기 기록 1",
    chip: (date: number) => `10월 ${date}일 오전 09:00에 운영 도우미 깨우기`,
    source: { at: "깨우기 · 10월 21일 오전 09:00에 깨우기", recurring: "깨우기 · 09:00 · 매일" } as Record<Choice, string>,
    steps: {
      read: "태스크, 댓글, 깨우기 지시 읽기",
      found: "견적 메일 확인: 3곳 모두 회신함",
      none: "견적 메일 확인: 아직 회신 없음",
      compare: "견적 비교표 만들기",
      checkin: "답할 내용이 없어 조용한 확인으로 종료",
      disable: "견적이 모였으니 이 정기 확인 끄기",
    },
    checkin: "조용한 확인: 3곳 모두 아직 견적 회신 없음. 내일 다시 확인",
    reply:
      "견적 3건이 모두 도착했습니다. A는 세트당 52만 원에 납기 10일, B는 48만 원에 14일, C는 55만 원에 7일입니다. 전시회까지 3주 남아서 B를 추천합니다. 비교표는 태스크에 첨부했습니다.",
    fastForward: (date: number) => `10월 ${date}일 오전 09:00으로 건너뛰기`,
    captions: {
      idle: "운영 도우미가 공급업체 3곳에 견적을 요청하고 회신을 기다리고 있습니다.",
      ready: "견적은 내일 전에는 오지 않습니다. 깨우기 섹션의 +를 클릭해 운영 도우미가 나중에 이 태스크로 돌아오게 하세요.",
      form: "깨울 대상에는 태스크 담당자인 운영 도우미가 미리 선택되어 있고, 지시도 적혀 있습니다. 조건 메뉴를 여세요.",
      menu: "여기서는 두 시간 조건 모두 쓸 수 있습니다. '특정 시각'은 한 번만, '정기 확인'은 종료일까지 매일 깨웁니다.",
      preset: "시간은 기본값이 1시간 후입니다. 내일 09:00으로 바꾸세요.",
      presetMenu: "'내일 09:00'을 선택하세요.",
      filled: {
        at: "내일 09:00에 운영 도우미를 한 번 깨웁니다. 만들기를 클릭하세요.",
        recurring: "10월 27일까지 매일 09:00에 운영 도우미를 깨웁니다. 만들기를 클릭하세요.",
      } as Record<Choice, string>,
      scheduled:
        "규칙이 태스크에 저장되고, 헤더에 이 태스크가 무엇을 기다리는지 표시됩니다. 그동안 돌아가는 실행은 없습니다. 깨우는 시각으로 건너뛰세요.",
      running: "09:00이 되었습니다. 운영 도우미가 깨우기 지시를 가지고 이 태스크에서 새 실행을 시작합니다.",
      checked:
        "아직 회신이 없습니다. 이번 실행은 댓글을 남기지 않고 타임라인에 조용한 확인 기록만 남겼습니다. 하루 더 건너뛰세요.",
      running2: "다음 날 확인이 시작됩니다.",
      done: {
        at: "운영 도우미가 견적을 읽고 비교를 마쳤습니다. 한 번만 깨우는 규칙은 트리거된 뒤 끝나고 기록으로 옮겨집니다. '다시 보기'로 다른 조건도 해 보세요.",
        recurring:
          "견적이 도착했습니다. 운영 도우미가 비교 결과를 올리고 정기 확인을 껐습니다. '다시 보기'로 다른 조건도 해 보세요.",
      } as Record<Choice, string>,
    },
  },
  fr: {
    title: "OPS-56 · Confirmer les devis fournisseurs pour le matériel du salon",
    agent: "Assistant ops",
    running: "En cours",
    completed: "Terminée",
    opening:
      "J'ai demandé un devis à 3 fournisseurs, avec une réponse attendue d'ici ce soir. Je ne peux pas comparer les prix avant d'avoir tous les devis, je m'arrête donc ici pour l'instant.",
    section: "Réveils",
    newWakeup: "Nouveau réveil",
    formTitle: "Nouveau réveil",
    when: "Quand",
    wake: "Réveiller",
    chooseCondition: "Choisir une condition",
    groupTime: "Temps",
    condAt: "À une heure donnée",
    condAtHint: "Dans 10 min, demain 09:00",
    condRecurring: "Vérifier régulièrement",
    condRecurringHint: "Toutes les heures ou chaque jour, avec une date de fin",
    atPresets: ["Dans 10 minutes", "Dans 1 heure", "Demain 09:00", "Choisir une heure"],
    daily: "Chaque jour 09:00",
    until: "Jusqu’au",
    untilValue: "27/10/2026",
    instructionTitle: "Action au réveil",
    instruction:
      "Consultez les e-mails de devis pour voir si les 3 fournisseurs ont répondu. Si c'est le cas, rassemblez les devis dans un tableau comparatif et recommandez-en un.",
    onBehalf: "Les exécutions agissent en votre nom",
    cancel: "Annuler",
    create: "Créer",
    rule: { at: "Réveil à 21 oct., 09:00", recurring: "À 09:00 · Tous les jours" } as Record<Choice, string>,
    summaryOnce: "Réveiller Assistant ops · Déclencher une fois",
    summaryFired: "Réveiller Assistant ops · Déclencher une fois · Déclenché",
    summaryNext: (date: number) =>
      `Réveiller Assistant ops · Prochain : ${date} oct., 09:00 · jusqu’au 27 oct.`,
    summaryOff: "Réveiller Assistant ops · Désactivé",
    thisRun: (done: boolean) => `Cette exécution : ${done ? "Exécution réussie" : "En cours"}`,
    lastRun: "Dernière exécution : Exécution réussie",
    history: "Historique des réveils 1",
    chip: (date: number) => `${date} oct., 09:00 · réveille Assistant ops`,
    source: { at: "Réveil · Réveil à 21 oct., 09:00", recurring: "Réveil · À 09:00 · Tous les jours" } as Record<Choice, string>,
    steps: {
      read: "Lire la tâche, les commentaires et l'instruction du réveil",
      found: "Consulter les e-mails de devis : les 3 ont répondu",
      none: "Consulter les e-mails de devis : aucune réponse pour l'instant",
      compare: "Préparer le tableau comparatif des devis",
      checkin: "Rien qui demande une réponse : terminer par une vérification silencieuse",
      disable: "Devis reçus : désactiver cette vérification régulière",
    },
    checkin: "vérification silencieuse : aucun des 3 fournisseurs n'a encore envoyé de devis ; nouvelle vérification demain",
    reply:
      "Les 3 devis sont arrivés : A à 260 € le lot, livré en 10 jours ; B à 240 € en 14 jours ; C à 275 € en 7 jours. Le salon a lieu dans 3 semaines, je recommande donc B. Le tableau comparatif est joint à la tâche.",
    fastForward: (date: number) => `Avancer au ${date} oct., 09:00`,
    captions: {
      idle: "Assistant ops a demandé des devis à 3 fournisseurs et attend leurs réponses.",
      ready: "Les devis n'arriveront pas avant demain. Cliquez sur + dans la section Réveils pour qu'Assistant ops revienne plus tard sur cette tâche.",
      form: "L'assigné de la tâche, Assistant ops, est déjà sélectionné et l'instruction est rédigée. Ouvrez le menu des conditions.",
      menu: "Les deux conditions de temps conviennent ici : « À une heure donnée » réveille Assistant ops une fois ; « Vérifier régulièrement » le réveille chaque jour jusqu'à la date de fin.",
      preset: "L'heure par défaut est dans une heure. Remplacez-la par demain 09:00.",
      presetMenu: "Choisissez « Demain 09:00 ».",
      filled: {
        at: "Assistant ops sera réveillé une fois, demain à 09:00. Cliquez sur Créer.",
        recurring: "Assistant ops sera réveillé chaque jour à 09:00 jusqu'au 27 oct. Cliquez sur Créer.",
      } as Record<Choice, string>,
      scheduled:
        "La règle est enregistrée sur la tâche, et l'en-tête indique ce que la tâche attend. Aucune exécution ne tourne entre-temps. Avancez jusqu'à l'heure du réveil.",
      running: "Il est 09:00 : Assistant ops démarre une nouvelle exécution sur cette tâche, avec l'instruction du réveil.",
      checked:
        "Pas encore de réponse. Cette exécution n'a publié aucun commentaire ; elle a seulement laissé une vérification silencieuse dans l'historique. Avancez d'un jour de plus.",
      running2: "La vérification du lendemain commence.",
      done: {
        at: "Assistant ops a trouvé les devis et terminé la comparaison. Un réveil unique s'arrête après s'être déclenché et passe dans l'historique. Utilisez Rejouer pour essayer l'autre condition.",
        recurring:
          "Les devis sont arrivés. Assistant ops a publié la comparaison et désactivé la vérification régulière. Utilisez Rejouer pour essayer l'autre condition.",
      } as Record<Choice, string>,
    },
  },
};

type Phase =
  | "idle"
  | "ready"
  | "form"
  | "menu"
  | "preset"
  | "presetMenu"
  | "filled"
  | "scheduled"
  | "running"
  | "checked"
  | "done";

const pillClass =
  "inline-flex h-7 max-w-full min-w-0 items-center gap-1.5 rounded-md border bg-background px-2 text-label outline-none transition-colors enabled:hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50 disabled:cursor-default [&_svg]:shrink-0";

/** The rule's on/off switch; shown, not operated, in this demo. */
function SwitchMark({ on }: { on: boolean }) {
  return (
    <span
      aria-hidden
      className={cn(
        "inline-flex h-4 w-7 shrink-0 items-center rounded-full p-0.5 transition-colors",
        on ? "bg-brand" : "bg-muted-foreground/30",
      )}
    >
      <span className={cn("size-3 rounded-full bg-background transition-transform", on && "translate-x-3")} />
    </span>
  );
}

function RuleRow({
  icon,
  title,
  on,
  children,
}: {
  icon: ReactNode;
  title: string;
  on: boolean;
  children: ReactNode;
}) {
  return (
    <div className="grid grid-cols-[minmax(0,1fr)_auto] gap-x-2 rounded-md py-1.5 text-caption motion-safe:animate-in motion-safe:fade-in">
      <span className="flex min-w-0 items-start gap-2 pl-2">
        {icon}
        <span className="min-w-0 font-medium break-words">{title}</span>
      </span>
      <span className="row-span-2 self-start pt-0.5 pr-2">
        <SwitchMark on={on} />
      </span>
      <span className="min-w-0 pl-7.5 break-words text-muted-foreground">{children}</span>
    </div>
  );
}

export function WakeupDemo() {
  const t = useDemoCopy(copy);
  const { after, clear } = useDemoTimers();
  const [frameRef, visible] = useStartWhenVisible<HTMLElement>();
  const [cycle, setCycle] = useState(0);
  const [phase, setPhase] = useState<Phase>("idle");
  const [choice, setChoice] = useState<Choice | null>(null);
  const [round, setRound] = useState<1 | 2>(1);
  const [stepsShown, setStepsShown] = useState(0);

  useEffect(() => {
    if (!visible) return;
    clear();
    after(600, () => setPhase("ready"));
  }, [visible, cycle, after, clear]);

  const stepsFor = (c: Choice, r: 1 | 2) =>
    c === "at"
      ? [t.steps.read, t.steps.found, t.steps.compare]
      : r === 1
        ? [t.steps.read, t.steps.none, t.steps.checkin]
        : [t.steps.read, t.steps.found, t.steps.compare, t.steps.disable];

  const pick = (c: Choice) => {
    setChoice(c);
    setPhase(c === "at" ? "preset" : "filled");
  };

  const fastForward = () => {
    if (!choice) return;
    const r: 1 | 2 = phase === "checked" ? 2 : 1;
    const steps = stepsFor(choice, r);
    setRound(r);
    setStepsShown(1);
    setPhase("running");
    steps.forEach((_, i) => {
      if (i > 0) after(i * 1000, () => setStepsShown(i + 1));
    });
    after(steps.length * 1000 + 300, () => setPhase(choice === "recurring" && r === 1 ? "checked" : "done"));
  };

  const replay = () => {
    clear();
    setPhase("idle");
    setChoice(null);
    setRound(1);
    setStepsShown(0);
    setCycle((c) => c + 1);
  };

  const shown: Phase = visible ? phase : "idle";
  const created = ["scheduled", "running", "checked", "done"].includes(shown) && choice !== null;
  const formOpen = ["form", "menu", "preset", "presetMenu", "filled"].includes(shown);
  const running = shown === "running";
  // A one-time rule turns off once it has fired; the regular check stays on
  // until the agent turns it off at the end.
  const ruleOn = created && (choice === "recurring" ? shown !== "done" : shown === "scheduled");
  const current = created && (ruleOn || running);
  // The day of the rule's next firing: the 21st until the first run starts.
  const next = shown === "scheduled" ? 21 : running && round === 2 ? 23 : 22;
  const steps = choice ? stepsFor(choice, round) : [];
  const showRun = choice !== null && (running || shown === "checked" || shown === "done");
  const showCheckin = choice === "recurring" && (shown === "checked" || round === 2);

  const caption = (() => {
    switch (shown) {
      case "filled":
        return t.captions.filled[choice ?? "at"];
      case "done":
        return t.captions.done[choice ?? "at"];
      case "running":
        return round === 2 ? t.captions.running2 : t.captions.running;
      default:
        return t.captions[shown];
    }
  })();

  const ruleLines = (() => {
    if (!choice) return null;
    if (choice === "at") {
      return (
        <>
          <span className="block">{shown === "scheduled" ? t.summaryOnce : t.summaryFired}</span>
          {shown !== "scheduled" && <span className="block">{t.thisRun(shown === "done")}</span>}
        </>
      );
    }
    return (
      <>
        <span className="block">{shown === "done" ? t.summaryOff : t.summaryNext(next)}</span>
        {running && <span className="block">{t.thisRun(false)}</span>}
        {(shown === "checked" || shown === "done") && <span className="block">{t.lastRun}</span>}
      </>
    );
  })();

  return (
    <DemoFrame frameRef={frameRef} title={t.title} caption={caption} onReplay={replay}>
      <div className="grid gap-4 p-4 sm:grid-cols-[minmax(0,1fr)_17rem]">
        {/* Issue: header and timeline */}
        <div className="flex min-w-0 flex-col gap-3">
          <div className="flex min-h-7 flex-wrap items-center gap-2">
            <span className="min-w-0 truncate text-body font-medium">{t.title}</span>
            {ruleOn && (
              <span
                key={next}
                className="inline-flex h-7 max-w-full min-w-0 items-center gap-1.5 rounded-full border px-2.5 text-caption motion-safe:animate-in motion-safe:fade-in"
              >
                <Bell aria-hidden className="size-3.5 shrink-0" />
                <span className="min-w-0 truncate">{t.chip(next)}</span>
              </span>
            )}
          </div>

          <DemoComment author={t.agent} agent>
            {t.opening}
          </DemoComment>

          {/* The check-in follows its own run, then stays above the next day's run. */}
          {showCheckin && round === 2 && (
            <div className="flex min-w-0 items-start gap-2 px-1 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
              <RefreshCw aria-hidden className="mt-px size-4 shrink-0" />
              <span className="min-w-0 flex-1 break-words">
                <span className="font-medium text-foreground">{t.agent}</span> {t.checkin}
              </span>
              <span className="hidden shrink-0 rounded-sm bg-muted px-1.5 py-0.5 text-micro sm:inline">
                {t.rule.recurring}
              </span>
            </div>
          )}

          {showRun && choice && (
            <div key={round} className="flex min-w-0 flex-col gap-1.5 motion-safe:animate-in motion-safe:fade-in">
              <span className="inline-flex w-fit max-w-full min-w-0 items-center gap-1 rounded-sm bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
                <Bell aria-hidden className="size-3 shrink-0" />
                <span className="truncate">{t.source[choice]}</span>
              </span>
              <DemoRun agent={t.agent} status={running ? t.running : t.completed} active={running}>
                {steps.slice(0, stepsShown).map((step, i) => (
                  <DemoStep key={step} state={running && i === stepsShown - 1 ? "running" : "done"}>
                    {step}
                  </DemoStep>
                ))}
              </DemoRun>
            </div>
          )}

          {showCheckin && round === 1 && (
            <div className="flex min-w-0 items-start gap-2 px-1 text-caption text-muted-foreground motion-safe:animate-in motion-safe:fade-in">
              <RefreshCw aria-hidden className="mt-px size-4 shrink-0" />
              <span className="min-w-0 flex-1 break-words">
                <span className="font-medium text-foreground">{t.agent}</span> {t.checkin}
              </span>
              <span className="hidden shrink-0 rounded-sm bg-muted px-1.5 py-0.5 text-micro sm:inline">
                {t.rule.recurring}
              </span>
            </div>
          )}

          {shown === "done" && (
            <DemoComment author={t.agent} agent>
              {t.reply}
            </DemoComment>
          )}

          {(shown === "scheduled" || shown === "checked") && (
            <div className="mt-auto">
              <CoachMark active side="right" ringClassName="rounded-md">
                <Button type="button" size="sm" variant="outline" onClick={fastForward}>
                  <FastForward />
                  {t.fastForward(shown === "checked" ? 22 : 21)}
                </Button>
              </CoachMark>
            </div>
          )}
        </div>

        {/* Issue sidebar: the Wakeups section */}
        <aside className="min-w-0 border-t pt-3 sm:border-t-0 sm:border-l sm:pt-0 sm:pl-4">
          <div className="mb-1 flex items-center gap-1">
            <span className="flex min-h-8 flex-1 items-center gap-1 px-2 text-caption font-medium">
              {t.section}
              <span className="text-muted-foreground tabular-nums">{current ? 1 : 0}</span>
              <ChevronRight aria-hidden className="size-3 rotate-90 text-muted-foreground" />
            </span>
            <CoachMark active={shown === "ready"} side="bottom" align="end" ringClassName="rounded-md">
              <button
                type="button"
                aria-label={t.newWakeup}
                title={t.newWakeup}
                disabled={shown !== "ready"}
                onClick={() => setPhase("form")}
                className="inline-flex size-6 items-center justify-center rounded-md text-muted-foreground outline-none enabled:hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50 disabled:opacity-50"
              >
                <Plus className="size-4" />
              </button>
            </CoachMark>
          </div>

          {formOpen && (
            <div className="rounded-lg border bg-popover p-3 text-popover-foreground shadow-md motion-safe:animate-in motion-safe:fade-in motion-safe:zoom-in-95">
              <div className="text-label font-semibold">{t.formTitle}</div>
              <div className="mt-3 grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-3 gap-y-2">
                <span className="flex h-7 items-center text-label text-muted-foreground">{t.when}</span>
                <div className="flex min-w-0 flex-wrap items-center gap-1.5">
                  <CoachMark active={shown === "form"} quiet ringClassName="rounded-md">
                    <button
                      type="button"
                      disabled={shown !== "form"}
                      onClick={() => setPhase("menu")}
                      className={cn(pillClass, !choice && "border-ring")}
                    >
                      {choice === "recurring" ? (
                        <RefreshCw aria-hidden className="size-3.5 text-muted-foreground" />
                      ) : (
                        <Clock3 aria-hidden className="size-3.5 text-muted-foreground" />
                      )}
                      <span className={cn("truncate", !choice && "text-muted-foreground")}>
                        {choice === "at" ? t.condAt : choice === "recurring" ? t.condRecurring : t.chooseCondition}
                      </span>
                      <ChevronDown aria-hidden className="size-3 text-muted-foreground" />
                    </button>
                  </CoachMark>
                  {choice === "at" && (
                    <CoachMark active={shown === "preset"} quiet ringClassName="rounded-md">
                      <button
                        type="button"
                        disabled={shown !== "preset"}
                        onClick={() => setPhase("presetMenu")}
                        className={pillClass}
                      >
                        <span className="truncate">{shown === "filled" ? t.atPresets[2] : t.atPresets[1]}</span>
                        <ChevronDown aria-hidden className="size-3 text-muted-foreground" />
                      </button>
                    </CoachMark>
                  )}
                  {choice === "recurring" && (
                    <span className={pillClass}>
                      <span className="truncate">{t.daily}</span>
                      <ChevronDown aria-hidden className="size-3 text-muted-foreground" />
                    </span>
                  )}
                </div>

                {shown === "menu" && (
                  <div className="col-span-2 rounded-md border bg-popover p-1 shadow-sm motion-safe:animate-in motion-safe:fade-in">
                    <div className="px-2 py-1 text-micro text-muted-foreground">{t.groupTime}</div>
                    {(
                      [
                        ["at", Clock3, t.condAt, t.condAtHint],
                        ["recurring", RefreshCw, t.condRecurring, t.condRecurringHint],
                      ] as const
                    ).map(([key, Icon, label, hint]) => (
                      <CoachMark key={key} active quiet ringClassName="rounded-sm" className="flex w-full">
                        <button
                          type="button"
                          onClick={() => pick(key)}
                          className="flex w-full items-start gap-2 rounded-sm px-2 py-1.5 text-left outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
                        >
                          <Icon aria-hidden className="mt-0.5 size-3.5 shrink-0 text-muted-foreground" />
                          <span className="flex min-w-0 flex-col gap-0.5">
                            <span className="text-label">{label}</span>
                            <span className="text-caption text-muted-foreground">{hint}</span>
                          </span>
                        </button>
                      </CoachMark>
                    ))}
                  </div>
                )}

                {shown === "presetMenu" && (
                  <div className="col-span-2 rounded-md border bg-popover p-1 shadow-sm motion-safe:animate-in motion-safe:fade-in">
                    {t.atPresets.map((label, i) =>
                      i === 2 ? (
                        <CoachMark key={label} active quiet ringClassName="rounded-sm" className="flex w-full">
                          <button
                            type="button"
                            onClick={() => setPhase("filled")}
                            className="flex w-full rounded-sm px-2 py-1 text-left text-label outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50"
                          >
                            {label}
                          </button>
                        </CoachMark>
                      ) : (
                        <span key={label} className="flex px-2 py-1 text-label text-muted-foreground">
                          {label}
                        </span>
                      ),
                    )}
                  </div>
                )}

                <span className="flex h-7 items-center text-label text-muted-foreground">{t.wake}</span>
                <span className={cn(pillClass, "w-fit")}>
                  <DemoAvatar name={t.agent} agent />
                  <span className="truncate">{t.agent}</span>
                  <ChevronDown aria-hidden className="size-3 text-muted-foreground" />
                </span>
              </div>

              <div className="mt-3 text-caption font-medium">{t.instructionTitle}</div>
              {/* Static text, not a textarea: the demo is scripted around this instruction. */}
              <div className="mt-1 rounded-md border bg-background px-2 py-1.5 text-label">{t.instruction}</div>

              {choice === "recurring" && (
                <div className="mt-3 flex items-center gap-3 text-caption">
                  <span className="text-muted-foreground">{t.until}</span>
                  <span className="rounded-md border bg-background px-2 py-1 tabular-nums">{t.untilValue}</span>
                </div>
              )}

              <div className="mt-3 flex flex-col gap-2 border-t pt-2.5">
                <span className="flex min-w-0 items-center gap-1 text-caption text-muted-foreground">
                  <Info aria-hidden className="size-3 shrink-0" />
                  {t.onBehalf}
                </span>
                <span className="flex items-center justify-end gap-1.5 sm:pb-7">
                  <Button type="button" size="sm" variant="ghost" disabled>
                    {t.cancel}
                  </Button>
                  <CoachMark active={shown === "filled"} side="bottom" align="end" ringClassName="rounded-lg">
                    <Button type="button" size="sm" disabled={shown !== "filled"} onClick={() => setPhase("scheduled")}>
                      {t.create}
                    </Button>
                  </CoachMark>
                </span>
              </div>
            </div>
          )}

          {created && choice && current && (
            <RuleRow
              icon={<Clock3 aria-hidden className="mt-px size-3.5 shrink-0 text-muted-foreground" />}
              title={t.rule[choice]}
              on={ruleOn}
            >
              {ruleLines}
            </RuleRow>
          )}

          {created && choice && !current && (
            <>
              <div className="flex min-h-8 items-center gap-1 px-2 text-caption text-muted-foreground">
                <ChevronRight aria-hidden className="size-3 rotate-90" />
                {t.history}
              </div>
              <RuleRow
                icon={<Clock3 aria-hidden className="mt-px size-3.5 shrink-0 text-muted-foreground" />}
                title={t.rule[choice]}
                on={false}
              >
                {ruleLines}
              </RuleRow>
            </>
          )}

        </aside>
      </div>
    </DemoFrame>
  );
}
