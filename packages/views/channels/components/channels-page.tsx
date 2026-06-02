"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  MessagesSquare,
  Plus,
  Hash,
  Lock,
  Send,
  Loader2,
  MessageCircleReply,
  Users,
  X,
  FileText,
} from "lucide-react";

import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { api } from "@multica/core/api";
import {
  channelKeys,
  channelListOptions,
  channelMessagesOptions,
  channelMembersOptions,
} from "@multica/core/channels";
import type {
  ChannelSummary,
  ChannelMessage,
  ChannelMember,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Badge } from "@multica/ui/components/ui/badge";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuTrigger,
} from "@multica/ui/components/ui/context-menu";
import { cn } from "@multica/ui/lib/utils";

import { useNavigation } from "../../navigation";

interface ChannelsPageProps {
  channelId?: string;
}

function formatTime(ts?: string): string {
  if (!ts) return "";
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString(undefined, {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}

function initialOf(name?: string | null): string {
  const n = (name ?? "").trim();
  return n ? n[0]!.toUpperCase() : "?";
}

export function ChannelsPage({ channelId }: ChannelsPageProps) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const nav = useNavigation();
  const qc = useQueryClient();
  const user = useAuthStore((s) => s.user);

  // State
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [showMembersDialog, setShowMembersDialog] = useState(false);
  const [threadMessageId, setThreadMessageId] = useState<string | null>(null);

  // Queries
  const { data: channels, isLoading: loadingChannels } = useQuery(channelListOptions(wsId));
  const { data: messages, isLoading: loadingMessages } = useQuery(channelMessagesOptions(wsId, channelId ?? null));
  const { data: members } = useQuery(channelMembersOptions(wsId, channelId ?? null));

  const activeChannel = useMemo(
    () => channels?.find((c) => c.id === channelId),
    [channels, channelId],
  );

  // Thread panel data
  const { data: threadData, isLoading: loadingThread } = useQuery({
    queryKey: channelKeys.messageThread(wsId, channelId ?? "", threadMessageId ?? ""),
    queryFn: () => api.getMessageThread(channelId!, threadMessageId!),
    enabled: !!channelId && !!threadMessageId,
  });

  return (
    <div className="flex h-full">
      {/* Left: Channel list sidebar */}
      <div className="flex w-56 shrink-0 flex-col border-r bg-muted/30">
        <div className="flex items-center justify-between border-b px-3 py-2">
          <h2 className="text-sm font-semibold">频道</h2>
          <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => setShowCreateDialog(true)}>
            <Plus className="h-4 w-4" />
          </Button>
        </div>
        <div className="flex-1 overflow-y-auto">
          {loadingChannels ? (
            <div className="space-y-2 p-3">
              {[1, 2, 3].map((i) => <Skeleton key={i} className="h-7 w-full" />)}
            </div>
          ) : (
            <ul className="space-y-0.5 p-1">
              {channels?.map((ch) => (
                <li key={ch.id}>
                  <button
                    onClick={() => nav.push(paths.channelDetail(ch.id))}
                    className={cn(
                      "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent",
                      ch.id === channelId && "bg-accent font-medium",
                    )}
                  >
                    {ch.access_mode === "invite" ? (
                      <Lock className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                    ) : (
                      <Hash className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                    )}
                    <span className="truncate">{ch.name}</span>
                    {ch.has_unread && <span className="ml-auto h-2 w-2 rounded-full bg-primary" />}
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>

      {/* Middle: Chat area */}
      <div className="flex min-w-0 flex-1 flex-col">
        {channelId && activeChannel ? (
          <>
            {/* Channel header */}
            <div className="flex items-center justify-between border-b px-4 py-2">
              <div className="flex items-center gap-2">
                <Hash className="h-4 w-4 text-muted-foreground" />
                <h1 className="text-base font-semibold">{activeChannel.name}</h1>
                {activeChannel.description && (
                  <span className="text-xs text-muted-foreground">{activeChannel.description}</span>
                )}
              </div>
              <Button variant="ghost" size="sm" onClick={() => setShowMembersDialog(true)}>
                <Users className="mr-1 h-4 w-4" />
                {members?.length ?? 0}
              </Button>
            </div>
            {/* Messages */}
            <MessageList
              messages={messages ?? []}
              loading={loadingMessages}
              channelId={channelId}
              wsId={wsId}
              onOpenThread={setThreadMessageId}
              qc={qc}
            />
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center text-muted-foreground">
            <div className="text-center">
              <MessagesSquare className="mx-auto mb-2 h-10 w-10 opacity-30" />
              <p className="text-sm">选择一个频道开始聊天</p>
            </div>
          </div>
        )}
      </div>

      {/* Right: Thread panel */}
      {threadMessageId && channelId && (
        <ThreadPanel
          channelId={channelId}
          messageId={threadMessageId}
          data={threadData}
          loading={loadingThread}
          wsId={wsId}
          qc={qc}
          onClose={() => setThreadMessageId(null)}
        />
      )}

      {/* Create channel dialog */}
      <CreateChannelDialog
        open={showCreateDialog}
        onClose={() => setShowCreateDialog(false)}
        wsId={wsId}
        qc={qc}
      />

      {/* Members dialog */}
      {channelId && (
        <MembersDialog
          open={showMembersDialog}
          onClose={() => setShowMembersDialog(false)}
          channelId={channelId}
          members={members ?? []}
          wsId={wsId}
          qc={qc}
        />
      )}
    </div>
  );
}

// ---- MessageList ----
function MessageList({
  messages,
  loading,
  channelId,
  wsId,
  onOpenThread,
  qc,
}: {
  messages: ChannelMessage[];
  loading: boolean;
  channelId: string;
  wsId: string;
  onOpenThread: (id: string) => void;
  qc: ReturnType<typeof useQueryClient>;
}) {
  const [input, setInput] = useState("");
  const bottomRef = useRef<HTMLDivElement>(null);
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [messages.length]);

  const sendMutation = useMutation({
    mutationFn: (content: string) => api.sendChannelMessage(channelId, { content }),
    onSuccess: () => {
      setInput("");
      qc.invalidateQueries({ queryKey: channelKeys.channelMessages(wsId, channelId) });
    },
    onError: () => toast.error("发送失败"),
  });

  const convertMutation = useMutation({
    mutationFn: (messageId: string) => api.convertMessageToIssue(channelId, messageId),
    onSuccess: (data) => {
      toast.success(`已创建 Issue #${data.issue_number}`);
      nav.push(paths.issueDetail(data.issue_id));
    },
    onError: () => toast.error("转换失败"),
  });

  const handleSend = useCallback(() => {
    const content = input.trim();
    if (!content) return;
    sendMutation.mutate(content);
  }, [input, sendMutation]);

  if (loading) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    );
  }

  return (
    <>
      <div className="flex-1 overflow-y-auto px-4 py-3">
        {messages.length === 0 ? (
          <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
            还没有消息，发第一条吧 👋
          </div>
        ) : (
          <ul className="space-y-3">
            {messages.map((msg) => (
              <ContextMenu key={msg.id}>
                <ContextMenuTrigger>
                  <li className="group flex items-start gap-3 rounded-md p-2 hover:bg-muted/50">
                    <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary/10 text-xs font-medium text-primary">
                      {initialOf(msg.author_name ?? msg.author_type)}
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className="flex items-baseline gap-2">
                        <span className="text-sm font-medium">
                          {msg.author_name ?? msg.author_type}
                        </span>
                        {msg.author_type === "agent" && (
                          <Badge variant="secondary" className="px-1.5 py-0 text-[10px]">Agent</Badge>
                        )}
                        <span className="text-xs text-muted-foreground">{formatTime(msg.created_at)}</span>
                      </div>
                      <p className="whitespace-pre-wrap break-words text-sm text-foreground/90">
                        {msg.content}
                      </p>
                      {(msg.reply_count ?? 0) > 0 && (
                        <button
                          onClick={() => onOpenThread(msg.id)}
                          className="mt-1 text-xs text-primary hover:underline"
                        >
                          {msg.reply_count} 条回复
                        </button>
                      )}
                    </div>
                  </li>
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ContextMenuItem onClick={() => onOpenThread(msg.id)}>
                    <MessageCircleReply className="mr-2 h-4 w-4" />
                    回复
                  </ContextMenuItem>
                  <ContextMenuItem onClick={() => convertMutation.mutate(msg.id)}>
                    <FileText className="mr-2 h-4 w-4" />
                    转换为 Issue
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            ))}
            <div ref={bottomRef} />
          </ul>
        )}
      </div>
      {/* Input */}
      <div className="border-t px-4 py-3">
        <div className="flex items-center gap-2">
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleSend();
              }
            }}
            placeholder="输入消息..."
            className="flex-1"
            disabled={sendMutation.isPending}
          />
          <Button size="icon" onClick={handleSend} disabled={!input.trim() || sendMutation.isPending}>
            {sendMutation.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : <Send className="h-4 w-4" />}
          </Button>
        </div>
      </div>
    </>
  );
}

// ---- Thread Panel ----
function ThreadPanel({
  channelId,
  messageId,
  data,
  loading,
  wsId,
  qc,
  onClose,
}: {
  channelId: string;
  messageId: string;
  data: any;
  loading: boolean;
  wsId: string;
  qc: ReturnType<typeof useQueryClient>;
  onClose: () => void;
}) {
  const [input, setInput] = useState("");

  const replyMutation = useMutation({
    mutationFn: (content: string) => api.replyToMessage(channelId, messageId, { content }),
    onSuccess: () => {
      setInput("");
      qc.invalidateQueries({ queryKey: channelKeys.messageThread(wsId, channelId, messageId) });
      qc.invalidateQueries({ queryKey: channelKeys.channelMessages(wsId, channelId) });
    },
    onError: () => toast.error("回复失败"),
  });

  const handleReply = useCallback(() => {
    const content = input.trim();
    if (!content) return;
    replyMutation.mutate(content);
  }, [input, replyMutation]);

  return (
    <div className="flex w-80 shrink-0 flex-col border-l bg-background">
      <div className="flex items-center justify-between border-b px-3 py-2">
        <h3 className="text-sm font-semibold">线程</h3>
        <Button variant="ghost" size="icon" className="h-6 w-6" onClick={onClose}>
          <X className="h-4 w-4" />
        </Button>
      </div>
      <div className="flex-1 overflow-y-auto px-3 py-2">
        {loading ? (
          <div className="flex items-center justify-center py-8">
            <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
          </div>
        ) : data ? (
          <div className="space-y-3">
            {/* Root message */}
            <div className="rounded-md border bg-muted/30 p-2">
              <div className="flex items-baseline gap-2">
                <span className="text-sm font-medium">
                  {data.root_message?.author_name ?? data.root_message?.author_type ?? ""}
                </span>
                <span className="text-xs text-muted-foreground">{formatTime(data.root_message?.created_at)}</span>
              </div>
              <p className="mt-1 whitespace-pre-wrap text-sm">{data.root_message?.content}</p>
            </div>
            {/* Replies */}
            {data.replies?.map((reply: ChannelMessage) => (
              <div key={reply.id} className="flex items-start gap-2 pl-2">
                <div className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-primary/10 text-[10px] font-medium text-primary">
                  {initialOf(reply.author_name ?? reply.author_type)}
                </div>
                <div className="min-w-0 flex-1">
                  <div className="flex items-baseline gap-1">
                    <span className="text-xs font-medium">{reply.author_name ?? reply.author_type}</span>
                    <span className="text-[10px] text-muted-foreground">{formatTime(reply.created_at)}</span>
                  </div>
                  <p className="whitespace-pre-wrap text-sm">{reply.content}</p>
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p className="py-4 text-center text-xs text-muted-foreground">还没有回复</p>
        )}
      </div>
      {/* Reply input */}
      <div className="border-t px-3 py-2">
        <div className="flex items-center gap-2">
          <Input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                handleReply();
              }
            }}
            placeholder="回复..."
            className="flex-1 text-sm"
            disabled={replyMutation.isPending}
          />
          <Button size="icon" className="h-8 w-8" onClick={handleReply} disabled={!input.trim() || replyMutation.isPending}>
            {replyMutation.isPending ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Send className="h-3.5 w-3.5" />}
          </Button>
        </div>
      </div>
    </div>
  );
}

// ---- Create Channel Dialog ----
function CreateChannelDialog({
  open,
  onClose,
  wsId,
  qc,
}: {
  open: boolean;
  onClose: () => void;
  wsId: string;
  qc: ReturnType<typeof useQueryClient>;
}) {
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [access, setAccess] = useState<"open" | "invite">("open");
  const nav = useNavigation();
  const paths = useWorkspacePaths();

  const createMutation = useMutation({
    mutationFn: () => api.createChannel({ name, description: desc || undefined, access_mode: access }),
    onSuccess: (ch) => {
      toast.success("频道已创建");
      qc.invalidateQueries({ queryKey: channelKeys.list(wsId) });
      onClose();
      setName("");
      setDesc("");
      nav.push(paths.channelDetail(ch.id));
    },
    onError: () => toast.error("创建失败"),
  });

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>创建频道</DialogTitle>
          <DialogDescription>创建一个新的频道来讨论话题</DialogDescription>
        </DialogHeader>
        <div className="space-y-3 py-2">
          <Input placeholder="频道名称" value={name} onChange={(e) => setName(e.target.value)} />
          <Input placeholder="描述（可选）" value={desc} onChange={(e) => setDesc(e.target.value)} />
          <div className="flex items-center gap-4 text-sm">
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={access === "open"} onChange={() => setAccess("open")} />
              公开
            </label>
            <label className="flex items-center gap-1.5">
              <input type="radio" checked={access === "invite"} onChange={() => setAccess("invite")} />
              邀请制
            </label>
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>取消</Button>
          <Button onClick={() => createMutation.mutate()} disabled={!name.trim() || createMutation.isPending}>
            {createMutation.isPending ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : null}
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// ---- Members Dialog ----
function MembersDialog({
  open,
  onClose,
  channelId,
  members,
  wsId,
  qc,
}: {
  open: boolean;
  onClose: () => void;
  channelId: string;
  members: ChannelMember[];
  wsId: string;
  qc: ReturnType<typeof useQueryClient>;
}) {
  const [addUserId, setAddUserId] = useState("");

  const addMutation = useMutation({
    mutationFn: () => api.addChannelMember(channelId, { user_id: addUserId }),
    onSuccess: () => {
      toast.success("成员已添加");
      setAddUserId("");
      qc.invalidateQueries({ queryKey: channelKeys.members(wsId, channelId) });
    },
    onError: () => toast.error("添加失败"),
  });

  const removeMutation = useMutation({
    mutationFn: (userId: string) => api.removeChannelMember(channelId, userId),
    onSuccess: () => {
      toast.success("成员已移除");
      qc.invalidateQueries({ queryKey: channelKeys.members(wsId, channelId) });
    },
    onError: () => toast.error("移除失败"),
  });

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>频道成员</DialogTitle>
          <DialogDescription>管理频道成员</DialogDescription>
        </DialogHeader>
        <div className="max-h-60 overflow-y-auto">
          <ul className="space-y-2">
            {members.map((m) => (
              <li key={m.user_id} className="flex items-center justify-between rounded-md px-2 py-1.5 hover:bg-muted/50">
                <div className="flex items-center gap-2">
                  <div className="flex h-7 w-7 items-center justify-center rounded-full bg-primary/10 text-xs font-medium text-primary">
                    {initialOf(m.user_name)}
                  </div>
                  <div>
                    <span className="text-sm">{m.user_name}</span>
                    <Badge variant="outline" className="ml-2 text-[10px]">{m.role}</Badge>
                  </div>
                </div>
                {m.role !== "owner" && (
                  <Button variant="ghost" size="sm" className="h-6 text-xs text-destructive" onClick={() => removeMutation.mutate(m.user_id)}>
                    移除
                  </Button>
                )}
              </li>
            ))}
          </ul>
        </div>
        <div className="flex items-center gap-2 pt-2">
          <Input
            placeholder="User ID"
            value={addUserId}
            onChange={(e) => setAddUserId(e.target.value)}
            className="flex-1 text-sm"
          />
          <Button size="sm" onClick={() => addMutation.mutate()} disabled={!addUserId.trim() || addMutation.isPending}>
            邀请
          </Button>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>关闭</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
