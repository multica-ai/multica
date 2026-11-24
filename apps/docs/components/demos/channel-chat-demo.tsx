"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { EditorialFigure, useDemoCopy } from "./demo-kit";

type Command = "mention" | "issue";

const copy = {
  en: {
    kicker: "CHANNEL BRIDGE",
    title: "How messages flow between your chat app and Multica",
    tabs: { mention: "@mention → Quick reply", issue: "/issue → Tracked issue" } as Record<Command, string>,
    channelZone: "SLACK / LARK / TELEGRAM / WECOM / DISCORD",
    multicaZone: "MULTICA WORKSPACE",
    msgMention: "@Engineer Summarize today's failed CI checks",
    msgIssue: "/issue Fix flaky checkout test on Safari",
    bridgeTitle: "Connected Bot",
    bridgeSub: "Routes group @mentions & commands",
    chatResultTitle: "Multica Chat Run",
    chatResultSub: "Replies directly back in the same thread",
    issueResultTitle: "Issue ENG-48 created",
    issueResultSub: "Assigned to Engineer · Run starts immediately",
    replyLoop: "Reply / Issue link posted back to channel",
    captions: {
      mention: "In a group chat, only messages that @mention the connected Bot trigger a run. The reply streams back into the same chat thread.",
      issue: "Prefix a message with `/issue` to create a tracked Multica issue straight from chat (first line = title, rest = description) and start the agent.",
    },
  },
  zh: {
    kicker: "渠道桥接",
    title: "外部聊天工具与 Multica 之间的消息流转",
    tabs: { mention: "@提及 → 群内直接回复", issue: "/issue → 建单并启动运行" } as Record<Command, string>,
    channelZone: "SLACK / 飞书 / 钉钉 / 企业微信 / TELEGRAM",
    multicaZone: "MULTICA 工作区",
    msgMention: "@Engineer 汇总今天失败的 CI 检查",
    msgIssue: "/issue 修复 Safari 结算页偶发测试失败",
    bridgeTitle: "已接入的智能体 Bot",
    bridgeSub: "仅响应群内 @提及 与斜杠命令",
    chatResultTitle: "进入 Multica 对话运行",
    chatResultSub: "完成后直接把回复发回当前群聊",
    issueResultTitle: "创建任务 ENG-48",
    issueResultSub: "自动指派给 Engineer 并立即开始执行",
    replyLoop: "结果回复 / 任务链接回传至原群聊",
    captions: {
      mention: "群聊中只有明确 @Bot 的消息才会触发智能体，普通聊天不会打扰它；结果直接回复在原群聊中。",
      issue: "使用 /issue 命令可直接把消息转为正式任务（首行为标题，其余为描述），自动分配给绑定的智能体并启动运行。",
    },
  },
  "zh-Hant": {
    kicker: "管道橋接",
    title: "外部聊天工具與 Multica 之間的訊息流轉",
    tabs: { mention: "@提及 → 群內回覆", issue: "/issue → 建立任務並執行" } as Record<Command, string>,
    channelZone: "SLACK / 飛書 / 釘釘 / 企業微信 / TELEGRAM",
    multicaZone: "MULTICA 工作區",
    msgMention: "@Engineer 彙整今天失敗的 CI 檢查",
    msgIssue: "/issue 修復 Safari 結帳頁偶發測試失敗",
    bridgeTitle: "已接入的智能體 Bot",
    bridgeSub: "僅回應群內 @提及 與指令",
    chatResultTitle: "進入 Multica 對話執行",
    chatResultSub: "完成後直接回覆至原群組",
    issueResultTitle: "建立任務 ENG-48",
    issueResultSub: "自動指派給 Engineer 並立即開始",
    replyLoop: "結果回覆 / 任務連結回傳至群組",
    captions: {
      mention: "群組中只有 @Bot 的訊息才會觸發智能體，結果直接回覆在原群組對話中。",
      issue: "使用 /issue 指令可直接建立正式任務，自動指派給綁定的智能體並啟動執行。",
    },
  },
  ja: {
    kicker: "CHANNEL BRIDGE",
    title: "チャットツールと Multica 間のメッセージ連携",
    tabs: { mention: "@メンション → スレッド返信", issue: "/issue → タスク作成・実行" } as Record<Command, string>,
    channelZone: "SLACK / LARK / TELEGRAM / DISCORD",
    multicaZone: "MULTICA ワークスペース",
    msgMention: "@Engineer 今日の失敗した CI をまとめて",
    msgIssue: "/issue Safari の決済テストの失敗を修正",
    bridgeTitle: "連携済み Bot",
    bridgeSub: "@メンションとコマンドのみ処理",
    chatResultTitle: "Multica チャット実行",
    chatResultSub: "同じスレッドへ返信を返す",
    issueResultTitle: "タスク ENG-48 を作成",
    issueResultSub: "Engineer に割り当てて即座に実行開始",
    replyLoop: "返信 / タスク URL をチャットへ返送",
    captions: {
      mention: "グループチャットでは Bot を @メンションしたメッセージだけがトリガーされ、返信が同じスレッドに戻ります。",
      issue: "/issue を使うとチャットから直接タスクを作成し、紐づくエージェントに割り当てて実行を始められます。",
    },
  },
  ko: {
    kicker: "CHANNEL BRIDGE",
    title: "외부 메신저와 Multica 간 메시지 흐름",
    tabs: { mention: "@멘션 → 스레드 답변", issue: "/issue → 태스크 생성·실행" } as Record<Command, string>,
    channelZone: "SLACK / LARK / TELEGRAM / DISCORD",
    multicaZone: "MULTICA 워크스페이스",
    msgMention: "@Engineer 오늘 실패한 CI 체크 요약해 줘",
    msgIssue: "/issue Safari 결제 테스트 간헐 실패 수정",
    bridgeTitle: "연결된 Bot",
    bridgeSub: "@멘션과 슬래시 명령만 라우팅",
    chatResultTitle: "Multica 채팅 실행",
    chatResultSub: "같은 채팅 스레드로 바로 답변",
    issueResultTitle: "태스크 ENG-48 생성",
    issueResultSub: "Engineer에 자동 할당 후 즉시 실행",
    replyLoop: "답변 / 태스크 링크를 원래 채널로 전달",
    captions: {
      mention: "그룹 채팅에서는 Bot을 @멘션한 메시지만 에이전트를 호출하며, 답변이 같은 스레드로 돌아옵니다.",
      issue: "/issue 명령으로 채팅에서 바로 태스크를 만들고 연결된 에이전트 실행을 시작할 수 있습니다.",
    },
  },
  fr: {
    kicker: "PASSERELLE CANAUX",
    title: "Échanges entre votre messagerie et Multica",
    tabs: { mention: "@mention → Réponse directe", issue: "/issue → Création de tâche" } as Record<Command, string>,
    channelZone: "SLACK / LARK / TELEGRAM / DISCORD",
    multicaZone: "ESPACE MULTICA",
    msgMention: "@Engineer Résume les échecs CI du jour",
    msgIssue: "/issue Corriger le test Safari instable",
    bridgeTitle: "Bot connecté",
    bridgeSub: "Traite @mentions et commandes",
    chatResultTitle: "Exécution Chat Multica",
    chatResultSub: "Répond dans le même fil de discussion",
    issueResultTitle: "Tâche ENG-48 créée",
    issueResultSub: "Assignée à Engineer · démarre aussitôt",
    replyLoop: "Réponse / lien de tâche renvoyé dans le canal",
    captions: {
      mention: "En groupe, seuls les messages qui @mentionnent le Bot déclenchent une exécution et répondent dans le fil.",
      issue: "La commande /issue crée une tâche suivie depuis le chat et lance l'agent lié.",
    },
  },
};

export function ChannelChatDemo() {
  const t = useDemoCopy(copy);
  const [cmd, setCmd] = useState<Command>("mention");
  const isIssue = cmd === "issue";

  return (
    <EditorialFigure
      kicker={t.kicker}
      title={t.title}
      actions={
        <div className="flex items-center gap-3" role="tablist" aria-label={t.title}>
          {(["mention", "issue"] as Command[]).map((m) => (
            <button
              key={m}
              type="button"
              role="tab"
              aria-selected={cmd === m}
              onClick={() => setCmd(m)}
              className={cn(
                "border-b-[1.5px] pb-1 text-[0.75rem] transition-colors",
                cmd === m
                  ? "border-[var(--primary)] font-medium text-foreground"
                  : "border-transparent text-muted-foreground hover:text-foreground",
              )}
            >
              {t.tabs[m]}
            </button>
          ))}
        </div>
      }
      caption={t.captions[cmd]}
    >
      <svg viewBox="0 0 640 212" className="block h-auto w-full select-none" role="img" aria-label={t.title}>
        <defs>
          <marker id="cc-hi" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="6" markerHeight="6" orient="auto">
            <path d="M0 1 L7 4 L0 7" fill="none" stroke="var(--primary)" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
        </defs>

        {/* Left: External Chat Channel */}
        <rect x="12" y="14" width="218" height="134" rx="5" fill="var(--background)" stroke="var(--border)" />
        <text x="24" y="33" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="8.5" letterSpacing="0.5">
          {t.channelZone}
        </text>
        <rect x="24" y="48" width="194" height="68" rx="4" fill="var(--accent)" stroke="var(--primary)" strokeWidth="1.4" />
        <text x="34" y="70" fill="var(--primary)" fontFamily="var(--font-mono)" fontSize="10" fontWeight="600">
          {isIssue ? "/issue" : "@mention"}
        </text>
        <text x="34" y="92" fill="var(--foreground)" fontSize="11" fontWeight="500">
          {(isIssue ? t.msgIssue : t.msgMention).slice(0, 28)}
        </text>

        {/* Middle: Bridge */}
        <path d="M230 82 H268" fill="none" stroke="var(--primary)" strokeWidth="1.6" markerEnd="url(#cc-hi)" />
        <rect x="270" y="50" width="126" height="64" rx="4" fill="var(--card)" stroke="var(--border)" />
        <text x="333" y="76" textAnchor="middle" fill="var(--foreground)" fontSize="12" fontWeight="600">
          {t.bridgeTitle}
        </text>
        <text x="333" y="96" textAnchor="middle" fill="var(--muted-foreground)" fontSize="9.5">
          {t.bridgeSub}
        </text>
        <path d="M396 82 H432" fill="none" stroke="var(--primary)" strokeWidth="1.6" markerEnd="url(#cc-hi)" />

        {/* Right: Multica Workspace Result */}
        <rect x="434" y="14" width="194" height="134" rx="5" fill="var(--background)" stroke="var(--border)" />
        <text x="446" y="33" fill="var(--muted-foreground)" fontFamily="var(--font-mono)" fontSize="9" letterSpacing="0.7">
          {t.multicaZone}
        </text>
        <rect x="446" y="48" width="170" height="68" rx="4" fill="var(--accent)" stroke="var(--primary)" strokeWidth="1.4" />
        <text x="456" y="72" fill="var(--foreground)" fontSize="12" fontWeight="600">
          {isIssue ? t.issueResultTitle : t.chatResultTitle}
        </text>
        <text x="456" y="94" fill="var(--muted-foreground)" fontSize="10">
          {(isIssue ? t.issueResultSub : t.chatResultSub).slice(0, 26)}
        </text>

        {/* Return loop */}
        <path
          d="M530 148 V186 H120 V150"
          fill="none"
          stroke="var(--primary)"
          strokeWidth="1.4"
          strokeDasharray="4 3"
          markerEnd="url(#cc-hi)"
        />
        <rect x="208" y="176" width="224" height="20" rx="3" fill="var(--card)" />
        <text x="320" y="189" textAnchor="middle" fill="var(--primary)" fontSize="10.5" fontWeight="500">
          {t.replyLoop}
        </text>
      </svg>
    </EditorialFigure>
  );
}
