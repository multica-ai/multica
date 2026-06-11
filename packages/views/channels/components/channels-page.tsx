"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { Bot, Hash, MessageCircle, Plus, Send, Smartphone, UserRound, X } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  channelsOptions,
  channelMessagesOptions,
  channelMembersOptions,
  useAddChannelMember,
  useCreateChannel,
  useRemoveChannelMember,
  useSendChannelMessage,
} from "@multica/core/channels";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import type { Channel, ChannelMember, ChannelMessage } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Badge } from "@multica/ui/components/ui/badge";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "../../layout/page-header";

function formatTime(value: string) {
  try {
    return new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(new Date(value));
  } catch {
    return "";
  }
}

function MessageRow({ message }: { message: ChannelMessage }) {
  const isExternal = message.source === "lark";
  const isAgent = message.author_type === "agent";
  return (
    <div className="group flex gap-3 rounded-xl px-3 py-2 hover:bg-muted/50">
      <div
        className={cn(
          "mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-full text-xs font-semibold",
          isExternal ? "bg-emerald-100 text-emerald-700" : isAgent ? "bg-blue-100 text-blue-700" : "bg-primary/10 text-primary",
        )}
      >
        {isExternal ? "飞" : isAgent ? <Bot className="size-4" /> : message.author_name.slice(0, 1).toUpperCase()}
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2 text-sm">
          <span className="font-medium">{message.author_name || "Unknown"}</span>
          {isExternal && <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">Feishu</Badge>}
          {isAgent && <Badge variant="secondary" className="h-5 px-1.5 text-[10px]">Agent</Badge>}
          <span className="text-xs text-muted-foreground">{formatTime(message.created_at)}</span>
        </div>
        <p className="mt-1 whitespace-pre-wrap break-words text-sm leading-6">{message.content}</p>
      </div>
    </div>
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="max-w-md rounded-3xl border bg-card p-8 text-center shadow-sm">
        <div className="mx-auto flex size-12 items-center justify-center rounded-2xl bg-primary/10 text-primary">
          <MessageCircle className="size-6" />
        </div>
        <h2 className="mt-5 text-xl font-semibold">创建一个群聊</h2>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">
          群聊是 Multica 里的即时通讯空间。把人和智能体拉进来，发消息时 @ 智能体，它会像群成员一样回复。
        </p>
        <Button className="mt-5" onClick={onCreate}><Plus className="size-4" /> 新建群聊</Button>
      </div>
    </div>
  );
}

function MemberPill({ member, onRemove }: { member: ChannelMember; onRemove: () => void }) {
  const isAgent = member.member_type === "agent";
  return (
    <span className="inline-flex items-center gap-1 rounded-full border bg-background px-2 py-1 text-xs">
      {isAgent ? <Bot className="size-3" /> : <UserRound className="size-3" />}
      <span className="max-w-32 truncate">{member.name || (isAgent ? "Agent" : "Member")}</span>
      <button type="button" onClick={onRemove} className="text-muted-foreground hover:text-foreground" aria-label="Remove member">
        <X className="size-3" />
      </button>
    </span>
  );
}

export function ChannelsPage() {
  const wsId = useWorkspaceId();
  const [activeId, setActiveId] = useState<string | null>(null);
  const [draft, setDraft] = useState("");
  const [newName, setNewName] = useState("");
  const [newLarkChatId, setNewLarkChatId] = useState("");
  const [selectedMember, setSelectedMember] = useState("");
  const [selectedAgent, setSelectedAgent] = useState("");
  const bottomRef = useRef<HTMLDivElement | null>(null);

  const { data: channels = [], isLoading } = useQuery(channelsOptions(wsId));
  const { data: workspaceMembers = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const active = useMemo(() => channels.find((c) => c.id === activeId) ?? channels[0] ?? null, [channels, activeId]);
  const { data: messages = [] } = useQuery(channelMessagesOptions(active?.id ?? ""));
  const { data: channelMembers = [] } = useQuery(channelMembersOptions(active?.id ?? ""));
  const createChannel = useCreateChannel();
  const sendMessage = useSendChannelMessage();
  const addMember = useAddChannelMember();
  const removeMember = useRemoveChannelMember();

  const memberIds = useMemo(() => new Set(channelMembers.filter((m) => m.member_type === "user").map((m) => m.member_id)), [channelMembers]);
  const agentIds = useMemo(() => new Set(channelMembers.filter((m) => m.member_type === "agent").map((m) => m.member_id)), [channelMembers]);
  const availableMembers = workspaceMembers.filter((m) => !memberIds.has(m.user_id));
  const availableAgents = agents.filter((a) => !agentIds.has(a.id) && !a.archived_at);

  useEffect(() => {
    if (!activeId && channels[0]) setActiveId(channels[0].id);
  }, [activeId, channels]);

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ block: "end" });
  }, [messages.length, active?.id]);

  const handleCreate = () => {
    const name = newName.trim() || "general";
    createChannel.mutate(
      { name, lark_chat_id: newLarkChatId.trim() || undefined },
      {
        onSuccess: (channel: Channel) => {
          setActiveId(channel.id);
          setNewName("");
          setNewLarkChatId("");
        },
      },
    );
  };

  const handleSend = () => {
    const content = draft.trim();
    if (!content || !active) return;
    sendMessage.mutate({ channelId: active.id, content }, { onSuccess: () => setDraft("") });
  };

  const addSelected = (memberType: "user" | "agent", memberId: string) => {
    if (!active || !memberId) return;
    addMember.mutate({ channelId: active.id, memberType, memberId });
    if (memberType === "user") setSelectedMember("");
    else setSelectedAgent("");
  };

  const appendMention = (name: string) => {
    const mention = `@${name}`;
    setDraft((prev) => (prev.trim() ? `${prev} ${mention} ` : `${mention} `));
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader className="h-auto min-h-12 px-5 py-2">
        <div>
          <h1 className="text-sm font-semibold">群聊</h1>
          <p className="text-xs text-muted-foreground">即时通讯空间：人类、智能体和可选的国内飞书群。</p>
        </div>
      </PageHeader>
      <div className="grid min-h-0 flex-1 grid-cols-[280px_1fr_300px] gap-0 border-t bg-background">
        <aside className="flex min-h-0 flex-col border-r bg-muted/20">
          <div className="border-b p-3">
            <div className="flex gap-2">
              <Input placeholder="群聊名称" value={newName} onChange={(e) => setNewName(e.target.value)} onKeyDown={(e) => { if (e.key === "Enter") handleCreate(); }} />
              <Button size="icon" onClick={handleCreate} disabled={createChannel.isPending} aria-label="Create channel">
                <Plus className="size-4" />
              </Button>
            </div>
            <Input className="mt-2" placeholder="可选飞书群 chat_id (oc_...)" value={newLarkChatId} onChange={(e) => setNewLarkChatId(e.target.value)} />
          </div>
          <div className="min-h-0 flex-1 overflow-y-auto p-2">
            {isLoading ? (
              <div className="space-y-2 p-2"><Skeleton className="h-10" /><Skeleton className="h-10" /></div>
            ) : channels.length === 0 ? (
              <div className="p-3 text-sm text-muted-foreground">还没有群聊。</div>
            ) : channels.map((channel) => (
              <button
                key={channel.id}
                type="button"
                onClick={() => setActiveId(channel.id)}
                className={cn(
                  "mb-1 flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-sm transition-colors",
                  active?.id === channel.id ? "bg-background shadow-sm" : "text-muted-foreground hover:bg-background/70 hover:text-foreground",
                )}
              >
                <Hash className="size-4" />
                <span className="min-w-0 flex-1 truncate">{channel.name}</span>
                {channel.lark_chat_id && <Smartphone className="size-3.5 text-emerald-600" />}
              </button>
            ))}
          </div>
        </aside>

        <main className="flex min-h-0 flex-col">
          {!active ? <EmptyState onCreate={handleCreate} /> : (
            <>
              <header className="flex items-center justify-between border-b px-5 py-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 font-semibold"><Hash className="size-4" /> {active.name}</div>
                  <p className="mt-0.5 text-xs text-muted-foreground">
                    {active.lark_chat_id ? `已绑定国内飞书群 ${active.lark_chat_id}` : "本地 Multica 群聊。创建时填入 oc_ chat_id 可同步到国内飞书群。"}
                  </p>
                </div>
                {active.lark_chat_id && <Badge variant="secondary">Feishu ready</Badge>}
              </header>
              <div className="min-h-0 flex-1 overflow-y-auto p-4">
                {messages.length === 0 ? (
                  <div className="flex h-full items-center justify-center text-sm text-muted-foreground">开始聊天吧。把智能体加入右侧成员后，输入 @智能体名称 触发回复。</div>
                ) : messages.map((message) => <MessageRow key={message.id} message={message} />)}
                <div ref={bottomRef} />
              </div>
              <div className="border-t bg-background p-4">
                <div className="flex items-end gap-2 rounded-2xl border bg-card p-2 shadow-sm">
                  <Textarea
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    placeholder="发消息到群聊。@智能体名称 会触发它回复。"
                    className="min-h-11 resize-none border-0 bg-transparent shadow-none focus-visible:ring-0"
                    onKeyDown={(e) => {
                      if ((e.metaKey || e.ctrlKey) && e.key === "Enter") handleSend();
                    }}
                  />
                  <Button onClick={handleSend} disabled={!draft.trim() || sendMessage.isPending}>
                    <Send className="size-4" /> 发送
                  </Button>
                </div>
              </div>
            </>
          )}
        </main>

        <aside className="flex min-h-0 flex-col border-l bg-muted/20">
          <div className="border-b p-4">
            <h2 className="text-sm font-semibold">成员</h2>
            <p className="mt-1 text-xs text-muted-foreground">把人和智能体拉进群。点击智能体可插入 @ 提及。</p>
          </div>
          {active && (
            <div className="min-h-0 flex-1 space-y-5 overflow-y-auto p-4">
              <div className="space-y-2">
                <div className="flex gap-2">
                  <select className="min-w-0 flex-1 rounded-md border bg-background px-2 text-sm" value={selectedMember} onChange={(e) => setSelectedMember(e.target.value)}>
                    <option value="">选择人类成员</option>
                    {availableMembers.map((m) => <option key={m.user_id} value={m.user_id}>{m.name || m.email}</option>)}
                  </select>
                  <Button size="sm" variant="outline" onClick={() => addSelected("user", selectedMember)} disabled={!selectedMember}>加入</Button>
                </div>
                <div className="flex gap-2">
                  <select className="min-w-0 flex-1 rounded-md border bg-background px-2 text-sm" value={selectedAgent} onChange={(e) => setSelectedAgent(e.target.value)}>
                    <option value="">选择智能体</option>
                    {availableAgents.map((a) => <option key={a.id} value={a.id}>{a.name}</option>)}
                  </select>
                  <Button size="sm" variant="outline" onClick={() => addSelected("agent", selectedAgent)} disabled={!selectedAgent}>加入</Button>
                </div>
              </div>

              <div className="space-y-2">
                <p className="text-xs font-medium text-muted-foreground">群成员</p>
                <div className="flex flex-wrap gap-2">
                  {channelMembers.length === 0 ? <span className="text-xs text-muted-foreground">暂无成员</span> : channelMembers.map((m) => (
                    <MemberPill
                      key={`${m.member_type}:${m.member_id}`}
                      member={m}
                      onRemove={() => removeMember.mutate({ channelId: active.id, memberType: m.member_type, memberId: m.member_id })}
                    />
                  ))}
                </div>
              </div>

              <div className="space-y-2">
                <p className="text-xs font-medium text-muted-foreground">快速 @ 智能体</p>
                <div className="space-y-1">
                  {channelMembers.filter((m) => m.member_type === "agent").map((m) => (
                    <button key={m.member_id} type="button" onClick={() => appendMention(m.name)} className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm hover:bg-background">
                      <Bot className="size-4 text-blue-600" /> @{m.name}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          )}
        </aside>
      </div>
    </div>
  );
}
