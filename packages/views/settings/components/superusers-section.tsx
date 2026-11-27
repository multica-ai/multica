"use client";

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { MemberWithUser } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { Button } from "@multica/ui/components/ui/button";
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@multica/ui/components/ui/alert-dialog";
import { useT } from "../../i18n";

export function SuperusersSection({ wsId, members, isOwner }: {
  wsId: string;
  members: MemberWithUser[];
  isOwner: boolean;
}) {
  const { t } = useT("settings");
  const qc = useQueryClient();
  const currentUser = useAuthStore((state) => state.user);
  const [selected, setSelected] = useState("");
  const [pendingGrant, setPendingGrant] = useState<string | null>(null);
  const policy = useQuery({
    queryKey: ["workspace-superusers", wsId],
    queryFn: () => api.listWorkspaceSuperusers(wsId),
    enabled: !!wsId,
  });
  const change = useMutation({
    mutationFn: ({ memberId, granted }: { memberId: string; granted: boolean }) =>
      api.setWorkspaceSuperuser(wsId, memberId, granted),
    onSuccess: () => {
      setSelected("");
      void qc.invalidateQueries({ queryKey: ["workspace-superusers", wsId] });
    },
  });
  const grants = policy.data?.grants ?? [];
  const available = members.filter((member) =>
    member.role !== "guest" && !grants.some((grant) => grant.member_id === member.id));
  const canInspectChats = grants.some((grant) => grant.user_id === currentUser?.id);

  return (
    <section className="space-y-3" aria-label={t(($) => $.members.superusers.title)}>
      <div>
        <h3 className="text-body font-medium">{t(($) => $.members.superusers.title)}</h3>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.members.superusers.description)}
        </p>
      </div>
      {policy.isLoading ? <p className="text-caption text-muted-foreground">{t(($) => $.members.superusers.loading)}</p> : null}
      {policy.isError ? <p role="alert" className="text-caption text-destructive">{t(($) => $.members.superusers.error)}</p> : null}
      {policy.data && (
        <>
          <p className="text-caption text-muted-foreground">
            {policy.data.enabled
              ? t(($) => $.members.superusers.enabled)
              : t(($) => $.members.superusers.disabled)}
          </p>
          {grants.length > 0 ? (
            <ul className="divide-y divide-border rounded-md border border-border">
              {grants.map((grant) => (
                <li key={grant.member_id} className="flex items-center justify-between gap-3 px-3 py-2">
                  <span className="min-w-0 truncate text-body">{grant.name} <span className="text-muted-foreground">({grant.email})</span></span>
                  {isOwner && <Button variant="outline" size="sm" disabled={change.isPending}
                    onClick={() => change.mutate({ memberId: grant.member_id, granted: false })}>
                    {t(($) => $.members.superusers.revoke)}
                  </Button>}
                </li>
              ))}
            </ul>
          ) : <p className="text-caption text-muted-foreground">{t(($) => $.members.superusers.none)}</p>}
          {isOwner && available.length > 0 && (
            <div className="flex items-center gap-2">
              <select className="h-9 min-w-0 rounded-md border border-input bg-background px-3 text-body"
                aria-label={t(($) => $.members.superusers.choose)} value={selected}
                onChange={(event) => setSelected(event.target.value)}>
                <option value="">{t(($) => $.members.superusers.choose)}</option>
                {available.map((member) => <option key={member.id} value={member.id}>{member.name} ({member.email})</option>)}
              </select>
              <Button size="sm" disabled={!selected || change.isPending}
                onClick={() => setPendingGrant(selected)}>
                {t(($) => $.members.superusers.grant)}
              </Button>
            </div>
          )}
          {change.isError && <p role="alert" className="text-caption text-destructive">{t(($) => $.members.superusers.error)}</p>}
        </>
      )}
      {canInspectChats && <SuperuserChatBrowser wsId={wsId} members={members} />}
      <AlertDialog open={pendingGrant !== null} onOpenChange={(open) => { if (!open) setPendingGrant(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.members.superusers.confirm_title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.members.superusers.confirm_description, {
                name: members.find((member) => member.id === pendingGrant)?.name ?? "",
              })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.members.superusers.confirm_cancel)}</AlertDialogCancel>
            <AlertDialogAction onClick={() => {
              if (pendingGrant) change.mutate({ memberId: pendingGrant, granted: true });
              setPendingGrant(null);
            }}>{t(($) => $.members.superusers.confirm_grant)}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

function SuperuserChatBrowser({ wsId, members }: { wsId: string; members: MemberWithUser[] }) {
  const { t } = useT("settings");
  const [input, setInput] = useState("");
  const [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState<string | null>(null);
  const chats = useQuery({
    queryKey: ["superuser-chats", wsId, query, offset],
    queryFn: () => api.listSuperuserChats(query, offset),
  });
  const messages = useQuery({
    queryKey: ["superuser-chat-messages", wsId, selected],
    queryFn: () => api.listSuperuserChatMessages(selected!),
    enabled: !!selected,
  });
  const memberName = (id: string) => members.find((member) => member.user_id === id)?.name ?? id;
  return (
    <div className="space-y-3 border-t border-border pt-4">
      <h4 className="text-body font-medium">{t(($) => $.members.superusers.chats_title)}</h4>
      <form className="flex gap-2" onSubmit={(event) => { event.preventDefault(); setQuery(input); setOffset(0); setSelected(null); }}>
        <input type="search" className="h-9 min-w-0 flex-1 rounded-md border border-input bg-background px-3 text-body"
          aria-label={t(($) => $.members.superusers.search)} placeholder={t(($) => $.members.superusers.search)}
          maxLength={200} value={input} onChange={(event) => setInput(event.target.value)} />
        <Button type="submit" variant="outline" size="sm">{t(($) => $.members.superusers.search_button)}</Button>
      </form>
      {chats.isError && <p role="alert" className="text-caption text-destructive">{t(($) => $.members.superusers.chats_error)}</p>}
      {chats.isLoading && <p className="text-caption text-muted-foreground">{t(($) => $.members.superusers.loading)}</p>}
      {chats.data && chats.data.length === 0 && <p className="text-caption text-muted-foreground">{t(($) => $.members.superusers.chats_empty)}</p>}
      {chats.data && chats.data.length > 0 && (
        <ul className="max-h-64 overflow-auto rounded-md border border-border">
          {chats.data.map((chat) => (
            <li key={chat.id} className="border-b border-border last:border-b-0">
              <button type="button" className="w-full px-3 py-2 text-left hover:bg-muted"
                aria-pressed={selected === chat.id} onClick={() => setSelected(chat.id)}>
                <span className="block truncate text-body">{chat.title || t(($) => $.members.superusers.untitled)}</span>
                <span className="block truncate text-caption text-muted-foreground">{memberName(chat.creator_id)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {(offset > 0 || chats.data?.length === 100) && <div className="flex justify-end gap-2">
        {offset > 0 && <Button variant="outline" size="sm" onClick={() => { setOffset(Math.max(0, offset - 100)); setSelected(null); }}>
          {t(($) => $.members.superusers.previous)}
        </Button>}
        {chats.data?.length === 100 && <Button variant="outline" size="sm" onClick={() => { setOffset(offset + 100); setSelected(null); }}>
          {t(($) => $.members.superusers.next)}
        </Button>}
      </div>}
      {selected && <div className="max-h-80 space-y-3 overflow-auto rounded-md border border-border p-3">
        {messages.isLoading && <p className="text-caption text-muted-foreground">{t(($) => $.members.superusers.loading)}</p>}
        {messages.isError && <p role="alert" className="text-caption text-destructive">{t(($) => $.members.superusers.chats_error)}</p>}
        {messages.data?.map((message) => <div key={message.id} className="space-y-1 border-b border-border pb-2 last:border-b-0">
          <span className="text-caption text-muted-foreground">{message.role}</span>
          <p className="whitespace-pre-wrap break-words text-body">{message.content}</p>
        </div>)}
      </div>}
    </div>
  );
}
