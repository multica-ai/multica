import { useCallback, useEffect, useRef, useState, type ReactNode, type Ref } from "react";
import { Bot, ChevronDown, Maximize2, Mic, Minus, Plus, SquarePen } from "@multica/ui/icons";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { cn } from "@multica/ui/lib/utils";
import { useStoryEnv } from "./env";

export interface DemoMessage {
  id: number;
  text: string;
  viaVoice?: boolean;
}

/** Messages the reviewer sends in a demo, appended under the seeded exchange. */
export function useDemoChat() {
  const [messages, setMessages] = useState<DemoMessage[]>([]);
  const send = useCallback((text: string, viaVoice = false) => {
    if (!text.trim()) return;
    setMessages((m) => [...m, { id: Date.now(), text: text.trim(), viaVoice }]);
  }, []);
  return { messages, send };
}

function AgentAvatar({ className }: { className?: string }) {
  return (
    <span className={cn("flex size-5 shrink-0 items-center justify-center rounded-full bg-brand/12 text-brand", className)}>
      <Bot className="size-3" />
    </span>
  );
}

function UserBubble({ message }: { message: DemoMessage }) {
  const { copy } = useStoryEnv();
  return (
    <div className="flex flex-col items-end gap-1">
      <div className="max-w-[80%] break-words rounded-2xl bg-muted px-3.5 py-2 text-body">{message.text}</div>
      {message.viaVoice && (
        <span className="flex items-center gap-1 pr-1 text-micro text-faint-foreground">
          <Mic className="size-3" />
          {copy.dictate}
        </span>
      )}
    </div>
  );
}

/**
 * The floating chat window (packages/views/chat/components/chat-window.tsx),
 * with a seeded exchange so the composer reads in context.
 */
export function ChatWindowFrame({
  children,
  messages = [],
  overlay,
  width = 460,
  height = 600,
  className,
}: {
  children: ReactNode;
  messages?: DemoMessage[];
  overlay?: ReactNode;
  width?: number;
  height?: number;
  className?: string;
}) {
  const { copy } = useStoryEnv();
  const listRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    listRef.current?.scrollTo({ top: listRef.current.scrollHeight, behavior: "smooth" });
  }, [messages.length]);

  return (
    <div
      className={cn(
        "@container relative flex flex-col overflow-hidden rounded-xl bg-surface-raised shadow-[var(--floating-shadow)] ring-1 ring-floating-border",
        className,
      )}
      style={{ width, height }}
    >
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5">
        <div className="flex min-w-0 items-center gap-1">
          <IconButton label="New chat" className="text-muted-foreground">
            <SquarePen />
          </IconButton>
          <span className="truncate text-body font-medium">{copy.sessionTitle}</span>
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          <IconButton label="Expand" className="text-muted-foreground">
            <Maximize2 />
          </IconButton>
          <IconButton label="Minimize" className="text-muted-foreground">
            <Minus />
          </IconButton>
        </div>
      </div>
      <div ref={listRef} className="min-h-0 flex-1 overflow-y-auto px-5">
        <div className="space-y-4 py-4">
          <UserBubble message={{ id: 0, text: copy.userMessage }} />
          <div className="text-body leading-relaxed">{copy.agentReply}</div>
          {messages.map((m) => (
            <UserBubble key={m.id} message={m} />
          ))}
        </div>
      </div>
      {children}
      {overlay}
    </div>
  );
}

/** The agent picker that sits at the composer's bottom-left. */
export function AgentPicker() {
  const { copy } = useStoryEnv();
  return (
    <button
      type="button"
      className="-ml-1 flex cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-1 outline-none transition-colors hover:bg-accent"
    >
      <AgentAvatar />
      <span className="max-w-28 truncate text-caption font-medium">{copy.agentName}</span>
      <ChevronDown className="size-3 shrink-0 text-muted-foreground" />
    </button>
  );
}

export function AddButton() {
  return (
    <IconButton label="Add" className="text-muted-foreground">
      <Plus />
    </IconButton>
  );
}

/**
 * The chat composer card (packages/views/chat/components/chat-input.tsx):
 * editor area on top, tools pinned to the bottom corners.
 */
export function ComposerSurface({
  children,
  above,
  left,
  right,
  bottom,
  active,
  className,
  surfaceRef,
}: {
  children: ReactNode;
  /** Rows between the editor and the toolbar, such as an inline error. */
  above?: ReactNode;
  left?: ReactNode;
  right?: ReactNode;
  /** Replaces the left corner with a row that runs up to `right` (or the edge without it). */
  bottom?: ReactNode;
  /** Draw the focus ring while voice is capturing, even without DOM focus. */
  active?: boolean;
  className?: string;
  surfaceRef?: Ref<HTMLDivElement>;
}) {
  return (
    <div className="relative z-10 flex flex-col px-5 pb-3 pt-0">
      <div
        ref={surfaceRef}
        className={cn(
          "relative flex min-h-16 max-h-96 flex-col rounded-lg border border-surface-border bg-surface pb-9 transition-[border-color,box-shadow] focus-within:border-brand focus-within:ring-2 focus-within:ring-ring/20",
          active && "border-brand ring-2 ring-ring/20",
          className,
        )}
      >
        <div className="min-h-0 flex-1 overflow-y-auto px-3 py-2 text-body">{children}</div>
        {above && <div className="px-2 pb-1">{above}</div>}
        {bottom ? (
          <div className={cn("absolute bottom-1 left-1.5 flex h-8 items-center gap-1", right ? "right-12" : "right-1.5")}>
            {bottom}
          </div>
        ) : (
          <div className="absolute bottom-1.5 left-1.5 flex items-center gap-1">{left}</div>
        )}
        {right && <div className="absolute bottom-1 right-1.5 flex items-center gap-1">{right}</div>}
      </div>
    </div>
  );
}

/** Plain text stand-in for the Tiptap editor: same type, same placeholder treatment. */
export function DraftEditor({
  value,
  onChange,
  placeholder,
  onSubmit,
  textareaRef,
  className,
}: {
  value: string;
  onChange: (value: string) => void;
  placeholder: string;
  onSubmit?: () => void;
  textareaRef?: Ref<HTMLTextAreaElement>;
  className?: string;
}) {
  return (
    <textarea
      ref={textareaRef}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={(e) => {
        if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
          e.preventDefault();
          onSubmit?.();
        }
      }}
      placeholder={placeholder}
      className={cn(
        "block max-h-40 min-h-5 w-full resize-none bg-transparent text-body outline-none field-sizing-content placeholder:text-muted-foreground",
        className,
      )}
    />
  );
}

export function SendButton({ onClick, disabled }: { onClick: () => void; disabled?: boolean }) {
  const { copy } = useStoryEnv();
  return <SubmitButton onClick={onClick} disabled={disabled} tooltip={copy.send} ariaLabel={copy.send} />;
}
