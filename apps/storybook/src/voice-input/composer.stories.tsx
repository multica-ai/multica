import type { ReactNode } from "react";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { ChatWindowFrame, useDemoChat } from "./chat-frame";
import { COMMENT_SCRIPT, type Lang } from "./copy";
import { useStoryEnv } from "./env";
import { Gallery, samples } from "./gallery";
import { ChatComposer, CommentComposer, IssuePage } from "./composer/surfaces";
import { VoiceProvider, type FrozenVoice } from "./composer/voice-provider";
import { ShortcutKeys } from "./composer/voice-ui";
import { useVoiceSettings } from "./settings/store";

const meta = {
  title: "Voice input/Composer (A + C)",
  id: "voice-input-composer",
  parameters: { layout: "fullscreen" },
} satisfies Meta;
export default meta;

const NOTES: Record<"chat" | "issue" | "together", Record<Lang, { title: string; pitch: string }>> = {
  chat: {
    zh: { title: "聊天输入框", pitch: "同一个麦克风按钮，点按是 A，按住是 C。" },
    en: { title: "Chat composer", pitch: "One mic button: a tap is A, a hold is C." },
  },
  issue: {
    zh: { title: "Issue 评论和回复", pitch: "评论框和讨论里的回复框接的是同一套语音层，行为完全一致。" },
    en: { title: "Issue comments and replies", pitch: "The comment box and the thread reply box plug into the same voice layer and behave identically." },
  },
  together: {
    zh: { title: "同时打开", pitch: "整个应用只有一个录音。快捷键把文字送进最近获得焦点的输入框，浮层就出现在那个输入框上方。" },
    en: {
      title: "Both open",
      pitch: "The app has one recording at a time. The shortcut dictates into the composer that last had focus, and the HUD appears above that composer.",
    },
  },
};

function Aside({ note }: { note: keyof typeof NOTES }) {
  const { lang, source } = useStoryEnv();
  const { settings } = useVoiceSettings();
  const n = NOTES[note][lang];
  const zh = lang === "zh";
  const keys = <ShortcutKeys keys={settings.shortcut} />;
  const steps: ReactNode[] = zh
    ? [
        <>点一下麦克风，或按一下 {keys}：底部变成录音条（A）。✓ 插入，↑ 直接发送，Esc 取消。</>,
        <>按住麦克风或 {keys} 说话：输入框上方浮出小条（C），松开就插入。</>,
        <>插入后会出现「润色」，一键把口语整理成书面语。</>,
      ]
    : [
        <>Tap the mic, or tap {keys}: the toolbar becomes the recording strip (A). ✓ inserts, ↑ sends, Esc cancels.</>,
        <>Hold the mic or {keys} and talk: a pill floats above the composer (C). Let go to insert.</>,
        <>After inserting, “Polish” turns speech into written text in one click.</>,
      ];
  return (
    <aside className="w-72 shrink-0 pt-2">
      <div className="text-caption font-medium text-brand">A + C</div>
      <h1 className="mt-1 text-title font-semibold">{n.title}</h1>
      <p className="mt-2 text-body text-muted-foreground">{n.pitch}</p>
      <ul className="mt-5 space-y-2.5 text-body">
        {steps.map((step, i) => (
          <li key={i} className="flex gap-2">
            <span className="mt-2 size-1 shrink-0 rounded-full bg-faint-foreground" />
            <span>{step}</span>
          </li>
        ))}
      </ul>
      <p className="mt-6 text-caption text-faint-foreground">
        {source === "simulated"
          ? zh
            ? "当前是模拟说话人。工具栏「Voice」切到「My microphone」可以用真麦克风试。"
            : "Using the simulated speaker. Switch Voice → My microphone in the toolbar to use your own voice."
          : zh
            ? "正在使用你的麦克风。"
            : "Using your microphone."}
      </p>
    </aside>
  );
}

function ChatWindow() {
  const chat = useDemoChat();
  return (
    <ChatWindowFrame messages={chat.messages}>
      <ChatComposer onSend={chat.send} />
    </ChatWindowFrame>
  );
}

export const Chat: StoryObj = {
  render: () => (
    <VoiceProvider primaryId="chat">
      <div className="flex items-start gap-10 p-8">
        <ChatWindow />
        <Aside note="chat" />
      </div>
    </VoiceProvider>
  ),
};

export const IssueComment: StoryObj = {
  name: "Issue comment",
  render: () => (
    <VoiceProvider primaryId="comment">
      <div className="flex items-start gap-10 p-8">
        <IssuePage />
        <Aside note="issue" />
      </div>
    </VoiceProvider>
  ),
};

export const Together: StoryObj = {
  name: "Chat and issue together",
  render: () => (
    <VoiceProvider primaryId="comment">
      <div className="flex items-start gap-6 p-8">
        <IssuePage width={640} height={680} />
        <ChatWindow />
        <Aside note="together" />
      </div>
    </VoiceProvider>
  ),
};

function Frozen({ state, children }: { state: FrozenVoice; children: ReactNode }) {
  return (
    <VoiceProvider primaryId={state.activeId ?? "chat"} frozen={state}>
      {children}
    </VoiceProvider>
  );
}

function StatesGallery() {
  const { lang } = useStoryEnv();
  const chat = samples(lang);
  const comment = samples(lang, COMMENT_SCRIPT[lang]);
  const zh = lang === "zh";
  const rows: { label: string; chat: FrozenVoice; comment: FrozenVoice; draft?: { chat: string; comment: string } }[] = [
    { label: zh ? "空闲" : "Idle", chat: { session: chat.idle, activeId: null }, comment: { session: comment.idle, activeId: null } },
    {
      label: zh ? "点按：录音条（A）" : "Tap: recording strip (A)",
      chat: { session: chat.listening, activeId: "chat", mode: "strip" },
      comment: { session: comment.listening, activeId: "comment", mode: "strip" },
    },
    {
      label: zh ? "按住：浮层（C）" : "Hold: floating pill (C)",
      chat: { session: chat.listening, activeId: "chat", mode: "hud" },
      comment: { session: comment.listening, activeId: "comment", mode: "hud" },
    },
    {
      label: zh ? "转写中" : "Transcribing",
      chat: { session: chat.processing, activeId: "chat", mode: "strip" },
      comment: { session: comment.processing, activeId: "comment", mode: "strip" },
    },
    {
      label: zh ? "已插入，提供润色" : "Inserted, Polish offered",
      chat: { session: chat.idle, activeId: null, offerFor: "chat" },
      comment: { session: comment.idle, activeId: null, offerFor: "comment" },
      draft: {
        chat: chat.processing.finalText,
        comment: comment.processing.finalText,
      },
    },
    {
      label: zh ? "没听清" : "Nothing heard",
      chat: { session: chat.noSpeech, activeId: "chat" },
      comment: { session: comment.noSpeech, activeId: "comment" },
    },
  ];
  return (
    <Gallery
      columns={2}
      items={rows.flatMap((row) => [
        {
          label: `${row.label} · ${zh ? "聊天" : "Chat"}`,
          node: (
            <Frozen state={row.chat}>
              <div className="w-[460px] pt-16">
                <ChatComposer onSend={() => {}} initialDraft={row.draft?.chat} />
              </div>
            </Frozen>
          ),
        },
        {
          label: `${row.label} · ${zh ? "Issue 评论" : "Issue comment"}`,
          node: (
            <Frozen state={row.comment}>
              <div className="w-[560px] pt-16">
                <CommentComposer onSend={() => {}} initialDraft={row.draft?.comment} />
              </div>
            </Frozen>
          ),
        },
      ])}
    />
  );
}

export const AllStates: StoryObj = { name: "All states", render: () => <StatesGallery /> };
