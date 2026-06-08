"use client";

import { Bot, ExternalLink, User } from "lucide-react";
import { useNavigation } from "@multica/views/navigation";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "@multica/ui/components/ui/sheet";
import { cn } from "@multica/ui/lib/utils";
import type { ActorMessage } from "../../core/api";

function formatDateTime(iso: string): string {
  try {
    return new Date(iso).toLocaleString(undefined, {
      weekday: "short",
      day: "2-digit",
      month: "short",
      year: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return "";
  }
}

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
    });
  } catch {
    return "";
  }
}

export function MessageDetailSheet({
  message,
  allMessages,
  workspaceSlug,
  onClose,
}: {
  message: ActorMessage | null;
  allMessages: ActorMessage[];
  workspaceSlug: string;
  onClose: () => void;
}) {
  const { push } = useNavigation();

  const sessionMessages = message
    ? allMessages
        .filter((m) => m.session_id === message.session_id)
        .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
    : [];

  return (
    <Sheet open={message !== null} onOpenChange={(open) => { if (!open) onClose(); }}>
      <SheetContent side="right" className="flex flex-col gap-0 p-0 sm:max-w-md">
        {message && (
          <>
            <SheetHeader className="shrink-0 border-b px-5 py-4">
              <div className="flex items-start justify-between gap-3 pr-8">
                <div className="min-w-0">
                  <SheetTitle className="text-sm font-semibold">
                    {message.sender_name ?? "Unknown"} → {message.agent_name}
                  </SheetTitle>
                  <SheetDescription className="text-xs text-muted-foreground">
                    {formatDateTime(message.created_at)}
                    {message.issue_title && (
                      <> · #{message.issue_number} {message.issue_title}</>
                    )}
                  </SheetDescription>
                </div>
              </div>
            </SheetHeader>

            {/* Meta row */}
            <div className="shrink-0 grid grid-cols-2 gap-x-4 border-b bg-muted/20 px-5 py-3">
              <div className="flex items-center gap-1.5 text-xs">
                <User className="size-3 shrink-0 text-muted-foreground" />
                <span className="text-muted-foreground">Fra</span>
                <span className="font-medium truncate">{message.sender_name ?? "—"}</span>
              </div>
              <div className="flex items-center gap-1.5 text-xs">
                <Bot className="size-3 shrink-0 text-muted-foreground" />
                <span className="text-muted-foreground">Til</span>
                <span className="font-medium truncate">{message.agent_name}</span>
              </div>
            </div>

            {/* Session messages — scrollable */}
            <div className="flex-1 min-h-0 overflow-y-auto">
              <div className="flex flex-col divide-y">
                {sessionMessages.length <= 1 ? (
                  <div className="p-5">
                    <div className="rounded-lg border bg-muted/30 px-4 py-3">
                      <p className="whitespace-pre-wrap text-sm leading-relaxed text-foreground/90">
                        {message.content}
                      </p>
                    </div>
                  </div>
                ) : (
                  <>
                    <div className="px-5 pt-4 pb-2">
                      <p className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
                        {sessionMessages.length} beskeder i denne session
                      </p>
                    </div>
                    {sessionMessages.map((msg) => (
                      <div
                        key={msg.id}
                        className={cn(
                          "px-5 py-3 transition-colors",
                          msg.id === message.id && "bg-accent/40"
                        )}
                      >
                        <div className="mb-1 flex items-center justify-between gap-2">
                          <span className="text-[11px] font-medium text-foreground">
                            {msg.sender_name ?? "Unknown"}
                          </span>
                          <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">
                            {formatTime(msg.created_at)}
                          </span>
                        </div>
                        <p className="whitespace-pre-wrap text-xs leading-relaxed text-foreground/80">
                          {msg.content}
                        </p>
                      </div>
                    ))}
                  </>
                )}
              </div>
            </div>

            {/* Footer */}
            <div className="shrink-0 border-t p-4">
              <button
                type="button"
                onClick={() => push(`/${workspaceSlug}/inbox?chat=${message.session_id}`)}
                className="flex w-full items-center justify-center gap-2 rounded-md border px-3 py-2 text-xs text-muted-foreground transition-colors hover:text-foreground"
              >
                <ExternalLink className="size-3.5" />
                Åbn hele samtalen i indbakken
              </button>
            </div>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}
