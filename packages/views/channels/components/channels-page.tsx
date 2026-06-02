"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  MessagesSquare,
  Plus,
  Hash,
  Lock,
  Send,
  FilePlus2,
  Loader2,
  ArrowLeft,
} from "lucide-react";

import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { api } from "@multica/core/api";
import {
  channelKeys,
  channelListOptions,
  channelThreadsOptions,
  threadMessagesOptions,
} from "@multica/core/channels";
import type {
  ChannelSummary,
  ChannelThreadSummary,
  ChannelMessage,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
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
import { cn } from "@multica/ui/lib/utils";

import { useNavigation } from "../../navigation";

interface ChannelsPageProps {
  channelId?: string;
}

const STATUS_LABEL: Record<string, string> = {
  todo: "待处理",
  in_progress: "进行中",
  in_review: "评审中",
  done: "已完成",
  blocked: "已阻塞",
  backlog: "待规划",
  cancelled: "已关闭",
};

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
  const user = useAuthStore((s) => s.user);
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const nav = useNavigation();
  const qc = useQueryClient();

  const [selectedThreadId, setSelectedThreadId] = useState<string | null>(null);
  const [createChannelOpen, setCreateChannelOpen] = useState(false);
  const [newChannelName, setNewChannelName] = useState("");
  const [newChannelDesc, setNewChannelDesc] = useState("");
  const [newChannelInvite, setNewChannelInvite] = useState(false);

  const [newThreadOpen, setNewThreadOpen] = useState(false);
  const [newThreadTitle, setNewThreadTitle] = useState("");
  const [newThreadContent, setNewThreadContent] = useState("");

  const [composeValue, setComposeValue] = useState("");

  const [issueDialogOpen, setIssueDialogOpen] = useState(false);
  const [issueTitle, setIssueTitle] = useState("");
  const [issueDesc, setIssueDesc] = useState("");

  const { data: channels = [], isLoading: channelsLoading } = useQuery(
    channelListOptions(wsId),
  );

  const selectedChannelId =
    channelId && channels.some((c) => c.id === channelId)
      ? channelId
      : channels[0]?.id ?? null;
  const selectedChannel =
    channels.find((c) => c.id === selectedChannelId) ?? null;

  const { data: threads = [], isLoading: threadsLoading } = useQuery(
    channelThreadsOptions(wsId, selectedChannelId),
  );

  const activeThreadId =
    selectedThreadId && threads.some((t) => t.id === selectedThreadId)
      ? selectedThreadId
      : null;

  const { data: threadData, isLoading: messagesLoading } = useQuery(
    threadMessagesOptions(wsId, selectedChannelId, activeThreadId),
  );

  const messages = threadData?.messages ?? [];
  const linkedIssues = threadData?.issues ?? [];
  const activeThread =
    threadData?.thread ?? threads.find((t) => t.id === activeThreadId) ?? null;

  const invalidateChannels = () =>
    qc.invalidateQueries({ queryKey: channelKeys.all(wsId) });

  function selectChannel(id: string) {
    setSelectedThreadId(null);
    nav.push(paths.channelDetail(id));
  }

  const createChannel = useMutation({
    mutationFn: () =>
      api.createChannel({
        name: newChannelName.trim(),
        description: newChannelDesc.trim() || undefined,
        access_mode: newChannelInvite ? "invite" : "open",
      }),
    onSuccess: (channel) => {
      setCreateChannelOpen(false);
      setNewChannelName("");
      setNewChannelDesc("");
      setNewChannelInvite(false);
      invalidateChannels();
      selectChannel(channel.id);
      toast.success("频道已创建");
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const joinChannel = useMutation({
    mutationFn: (id: string) => api.joinChannel(id),
    onSuccess: () => {
      invalidateChannels();
      toast.success("已加入频道");
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const createThread = useMutation({
    mutationFn: () =>
      api.createChannelThread(selectedChannelId!, {
        title: newThreadTitle.trim() || undefined,
        content: newThreadContent.trim() || undefined,
      }),
    onSuccess: (thread) => {
      setNewThreadOpen(false);
      setNewThreadTitle("");
      setNewThreadContent("");
      invalidateChannels();
      setSelectedThreadId(thread.id);
      toast.success("线程已创建");
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const sendMessage = useMutation({
    mutationFn: (content: string) =>
      api.createChannelMessage(selectedChannelId!, activeThreadId!, { content }),
    onSuccess: () => {
      setComposeValue("");
      qc.invalidateQueries({
        queryKey: channelKeys.messages(
          wsId,
          selectedChannelId ?? "",
          activeThreadId ?? "",
        ),
      });
      invalidateChannels();
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const createIssue = useMutation({
    mutationFn: () =>
      api.createIssue({
        title: issueTitle.trim(),
        description: issueDesc.trim() || undefined,
        source_thread_id: activeThreadId!,
      }),
    onSuccess: (issue) => {
      setIssueDialogOpen(false);
      setIssueTitle("");
      setIssueDesc("");
      qc.invalidateQueries({
        queryKey: channelKeys.messages(
          wsId,
          selectedChannelId ?? "",
          activeThreadId ?? "",
        ),
      });
      invalidateChannels();
      toast.success("已从线程创建 Issue");
      nav.push(paths.issueDetail(issue.identifier ?? issue.id));
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const canPost = selectedChannel
    ? selectedChannel.access_mode === "open" || selectedChannel.is_member
    : false;

  function openIssueDialog() {
    const firstLine = messages[0]?.content?.split("\n")[0]?.slice(0, 80) ?? "";
    setIssueTitle(activeThread?.title || firstLine || "");
    setIssueDesc("");
    setIssueDialogOpen(true);
  }

  return (
    <div className="flex h-full min-h-0">
      {/* Channel list */}
      <aside className="flex w-60 shrink-0 flex-col border-r bg-muted/20">
        <div className="flex items-center justify-between px-4 py-3">
          <div className="flex items-center gap-2 font-medium">
            <MessagesSquare className="size-4" />
            频道
          </div>
          <Button
            size="icon"
            variant="ghost"
            className="size-7"
            onClick={() => setCreateChannelOpen(true)}
            aria-label="创建频道"
          >
            <Plus className="size-4" />
          </Button>
        </div>
        <div className="flex-1 overflow-y-auto px-2 pb-3">
          {channelsLoading ? (
            <div className="space-y-2 px-2">
              {[0, 1, 2].map((i) => (
                <Skeleton key={i} className="h-8 w-full" />
              ))}
            </div>
          ) : channels.length === 0 ? (
            <p className="px-2 py-6 text-center text-sm text-muted-foreground">
              还没有频道，点击右上角创建。
            </p>
          ) : (
            <ul className="space-y-0.5">
              {channels.map((c) => (
                <ChannelRow
                  key={c.id}
                  channel={c}
                  active={c.id === selectedChannelId}
                  onSelect={() => selectChannel(c.id)}
                />
              ))}
            </ul>
          )}
        </div>
      </aside>

      {/* Thread list */}
      <section className="flex w-72 shrink-0 flex-col border-r">
        {selectedChannel ? (
          <>
            <header className="flex items-center justify-between gap-2 border-b px-4 py-3">
              <div className="min-w-0">
                <div className="flex items-center gap-1.5 font-medium">
                  {selectedChannel.access_mode === "invite" ? (
                    <Lock className="size-3.5 text-muted-foreground" />
                  ) : (
                    <Hash className="size-3.5 text-muted-foreground" />
                  )}
                  <span className="truncate">{selectedChannel.name}</span>
                </div>
                {selectedChannel.description ? (
                  <p className="truncate text-xs text-muted-foreground">
                    {selectedChannel.description}
                  </p>
                ) : null}
              </div>
              {!selectedChannel.is_member &&
              selectedChannel.access_mode === "invite" ? (
                <Button
                  size="sm"
                  variant="outline"
                  disabled={joinChannel.isPending}
                  onClick={() => joinChannel.mutate(selectedChannel.id)}
                >
                  加入
                </Button>
              ) : (
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-7"
                  disabled={!canPost}
                  onClick={() => setNewThreadOpen(true)}
                  aria-label="新建线程"
                >
                  <Plus className="size-4" />
                </Button>
              )}
            </header>
            <div className="flex-1 overflow-y-auto">
              {threadsLoading ? (
                <div className="space-y-2 p-3">
                  {[0, 1, 2].map((i) => (
                    <Skeleton key={i} className="h-12 w-full" />
                  ))}
                </div>
              ) : threads.length === 0 ? (
                <p className="px-4 py-8 text-center text-sm text-muted-foreground">
                  还没有线程。开一个新线程开始讨论吧。
                </p>
              ) : (
                <ul>
                  {threads.map((t) => (
                    <ThreadRow
                      key={t.id}
                      thread={t}
                      active={t.id === activeThreadId}
                      onSelect={() => setSelectedThreadId(t.id)}
                    />
                  ))}
                </ul>
              )}
            </div>
          </>
        ) : (
          <div className="flex flex-1 items-center justify-center p-6 text-sm text-muted-foreground">
            选择或创建一个频道
          </div>
        )}
      </section>

      {/* Messages */}
      <main className="flex min-w-0 flex-1 flex-col">
        {activeThread ? (
          <>
            <header className="flex items-center justify-between gap-3 border-b px-5 py-3">
              <div className="flex min-w-0 items-center gap-2">
                <Button
                  size="icon"
                  variant="ghost"
                  className="size-7 md:hidden"
                  onClick={() => setSelectedThreadId(null)}
                  aria-label="返回"
                >
                  <ArrowLeft className="size-4" />
                </Button>
                <h2 className="truncate font-medium">
                  {activeThread.title || "未命名线程"}
                </h2>
              </div>
              <Button
                size="sm"
                variant="outline"
                onClick={openIssueDialog}
                className="gap-1.5"
              >
                <FilePlus2 className="size-4" />
                建 Issue
              </Button>
            </header>

            {linkedIssues.length > 0 ? (
              <div className="flex flex-wrap gap-2 border-b bg-muted/20 px-5 py-2">
                {linkedIssues.map((iss) => (
                  <button
                    key={iss.id}
                    type="button"
                    onClick={() => nav.push(paths.issueDetail(iss.id))}
                    className="inline-flex items-center gap-1.5 rounded-md border bg-background px-2 py-1 text-xs hover:bg-accent"
                  >
                    <span className="font-mono text-muted-foreground">
                      #{iss.number}
                    </span>
                    <span className="max-w-50 truncate">{iss.title}</span>
                    <Badge variant="secondary" className="ml-0.5">
                      {STATUS_LABEL[iss.status] ?? iss.status}
                    </Badge>
                  </button>
                ))}
              </div>
            ) : null}

            <div className="flex-1 overflow-y-auto px-5 py-4">
              {messagesLoading ? (
                <div className="space-y-4">
                  {[0, 1, 2].map((i) => (
                    <Skeleton key={i} className="h-14 w-2/3" />
                  ))}
                </div>
              ) : messages.length === 0 ? (
                <p className="py-8 text-center text-sm text-muted-foreground">
                  还没有消息。
                </p>
              ) : (
                <ul className="space-y-4">
                  {messages.map((m) => (
                    <MessageRow
                      key={m.id}
                      message={m}
                      isSelf={!!user && m.author_id === user.id}
                    />
                  ))}
                </ul>
              )}
            </div>

            <div className="border-t p-3">
              <div className="flex items-end gap-2">
                <Textarea
                  value={composeValue}
                  onChange={(e) => setComposeValue(e.target.value)}
                  onKeyDown={(e) => {
                    if (
                      (e.metaKey || e.ctrlKey) &&
                      e.key === "Enter" &&
                      composeValue.trim() &&
                      !sendMessage.isPending
                    ) {
                      e.preventDefault();
                      sendMessage.mutate(composeValue.trim());
                    }
                  }}
                  placeholder={canPost ? "输入消息，⌘/Ctrl + Enter 发送" : "该频道已锁定或需加入后才能发言"}
                  disabled={!canPost || sendMessage.isPending}
                  rows={2}
                  className="resize-none"
                />
                <Button
                  size="icon"
                  disabled={!canPost || !composeValue.trim() || sendMessage.isPending}
                  onClick={() => sendMessage.mutate(composeValue.trim())}
                  aria-label="发送"
                >
                  {sendMessage.isPending ? (
                    <Loader2 className="size-4 animate-spin" />
                  ) : (
                    <Send className="size-4" />
                  )}
                </Button>
              </div>
            </div>
          </>
        ) : (
          <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
            <MessagesSquare className="size-8" />
            <p className="text-sm">选择一个线程查看对话</p>
          </div>
        )}
      </main>

      {/* Create channel dialog */}
      <Dialog open={createChannelOpen} onOpenChange={setCreateChannelOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建频道</DialogTitle>
            <DialogDescription>
              频道是持久的人机协作上下文容器，内部由线程组织讨论。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <Input
              placeholder="频道名称"
              value={newChannelName}
              onChange={(e) => setNewChannelName(e.target.value)}
              autoFocus
            />
            <Textarea
              placeholder="频道描述（可选）"
              value={newChannelDesc}
              onChange={(e) => setNewChannelDesc(e.target.value)}
              rows={2}
            />
            <label className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                checked={newChannelInvite}
                onChange={(e) => setNewChannelInvite(e.target.checked)}
              />
              仅邀请可见（默认全员可见）
            </label>
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateChannelOpen(false)}>
              取消
            </Button>
            <Button
              disabled={!newChannelName.trim() || createChannel.isPending}
              onClick={() => createChannel.mutate()}
            >
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* New thread dialog */}
      <Dialog open={newThreadOpen} onOpenChange={setNewThreadOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建线程</DialogTitle>
          </DialogHeader>
          <div className="space-y-3">
            <Input
              placeholder="线程标题（可选）"
              value={newThreadTitle}
              onChange={(e) => setNewThreadTitle(e.target.value)}
              autoFocus
            />
            <Textarea
              placeholder="开场消息（可选）"
              value={newThreadContent}
              onChange={(e) => setNewThreadContent(e.target.value)}
              rows={3}
            />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setNewThreadOpen(false)}>
              取消
            </Button>
            <Button
              disabled={
                (!newThreadTitle.trim() && !newThreadContent.trim()) ||
                createThread.isPending
              }
              onClick={() => createThread.mutate()}
            >
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Create issue dialog */}
      <Dialog open={issueDialogOpen} onOpenChange={setIssueDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>从线程创建 Issue</DialogTitle>
            <DialogDescription>
              新 Issue 会与本线程双向关联，Issue 状态变更会回流到这里。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <Input
              placeholder="Issue 标题"
              value={issueTitle}
              onChange={(e) => setIssueTitle(e.target.value)}
              autoFocus
            />
            <Textarea
              placeholder="描述（可选）"
              value={issueDesc}
              onChange={(e) => setIssueDesc(e.target.value)}
              rows={4}
            />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setIssueDialogOpen(false)}>
              取消
            </Button>
            <Button
              disabled={!issueTitle.trim() || createIssue.isPending}
              onClick={() => createIssue.mutate()}
            >
              创建 Issue
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function ChannelRow({
  channel,
  active,
  onSelect,
}: {
  channel: ChannelSummary;
  active: boolean;
  onSelect: () => void;
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        className={cn(
          "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-accent",
          active && "bg-accent font-medium",
        )}
      >
        {channel.access_mode === "invite" ? (
          <Lock className="size-3.5 shrink-0 text-muted-foreground" />
        ) : (
          <Hash className="size-3.5 shrink-0 text-muted-foreground" />
        )}
        <span className="min-w-0 flex-1 truncate">{channel.name}</span>
        {channel.has_unread ? (
          <span className="size-2 shrink-0 rounded-full bg-emerald-500" />
        ) : null}
      </button>
    </li>
  );
}

function ThreadRow({
  thread,
  active,
  onSelect,
}: {
  thread: ChannelThreadSummary;
  active: boolean;
  onSelect: () => void;
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onSelect}
        className={cn(
          "flex w-full flex-col gap-1 border-b px-4 py-3 text-left hover:bg-accent/50",
          active && "bg-accent/60",
        )}
      >
        <span className="truncate text-sm font-medium">
          {thread.title || "未命名线程"}
        </span>
        <span className="flex items-center gap-2 text-xs text-muted-foreground">
          <span>{thread.message_count} 条消息</span>
          {thread.issue_count > 0 ? (
            <span className="text-brand">· {thread.issue_count} Issue</span>
          ) : null}
          <span className="ml-auto">{formatTime(thread.last_message_at)}</span>
        </span>
      </button>
    </li>
  );
}

function MessageRow({
  message,
  isSelf,
}: {
  message: ChannelMessage;
  isSelf: boolean;
}) {
  if (message.author_type === "system") {
    return (
      <li className="flex justify-center">
        <span className="rounded-full bg-muted px-3 py-1 text-xs text-muted-foreground">
          {message.content}
        </span>
      </li>
    );
  }
  const name =
    message.author_name ||
    (message.author_type === "agent" ? "Agent" : isSelf ? "我" : "成员");
  return (
    <li className="flex gap-3">
      <div
        className={cn(
          "flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-medium text-white",
          message.author_type === "agent" ? "bg-violet-500" : "bg-sky-500",
        )}
      >
        {initialOf(name)}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <span className="text-sm font-medium">{name}</span>
          {message.author_type === "agent" ? (
            <Badge variant="secondary" className="px-1.5 py-0 text-[10px]">
              Agent
            </Badge>
          ) : null}
          <span className="text-xs text-muted-foreground">
            {formatTime(message.created_at)}
          </span>
        </div>
        <p className="whitespace-pre-wrap break-words text-sm text-foreground/90">
          {message.content}
        </p>
      </div>
    </li>
  );
}
