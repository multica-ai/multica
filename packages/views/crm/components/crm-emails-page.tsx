"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Mail, Search } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { crmAccountListOptions, crmContactListOptions, crmEmailMessageListOptions, crmEmailThreadListOptions, crmKeys } from "@multica/core/crm/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import type { CRMEmailThread } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

function messageTime(value?: string | null) {
  return value ? new Date(value).toLocaleString() : "—";
}

export function CRMEmailsPage() {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const { t } = useT("crm");
  const [search, setSearch] = useState("");
  const [selectedThreadId, setSelectedThreadId] = useState<string | null>(null);
  const [selectedAccountId, setSelectedAccountId] = useState("");
  const [selectedContactId, setSelectedContactId] = useState("");
  const { data: threads = [], isLoading } = useQuery(crmEmailThreadListOptions(wsId));
  const { data: accounts = [] } = useQuery(crmAccountListOptions(wsId, { sort: "name" }));

  const filteredThreads = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return threads;
    return threads.filter((thread) => [thread.subject, thread.mailbox, thread.direction, thread.status]
      .filter(Boolean)
      .join(" ")
      .toLowerCase()
      .includes(q));
  }, [search, threads]);

  const selectedThread = useMemo<CRMEmailThread | null>(() => {
    const found = threads.find((thread) => thread.id === selectedThreadId) ?? filteredThreads[0] ?? null;
    return found;
  }, [filteredThreads, selectedThreadId, threads]);

  const effectiveAccountId = selectedAccountId || selectedThread?.account_id || "";
  const { data: contacts = [] } = useQuery({
    ...crmContactListOptions(wsId, effectiveAccountId),
    enabled: Boolean(effectiveAccountId),
  });
  const { data: messages = [], isLoading: messagesLoading } = useQuery({
    ...crmEmailMessageListOptions(wsId, selectedThread?.id ?? ""),
    enabled: Boolean(selectedThread?.id),
  });

  const selectedAccount = accounts.find((account) => account.id === effectiveAccountId) ?? null;
  const selectedContact = contacts.find((contact) => contact.id === (selectedContactId || selectedThread?.contact_id || "")) ?? null;

  const updateAssociation = useMutation({
    mutationFn: async () => {
      if (!selectedThread) throw new Error("No email thread selected");
      return api.updateCRMEmailThreadAssociation(selectedThread.id, {
        account_id: effectiveAccountId || null,
        contact_id: selectedContactId || selectedThread.contact_id || null,
      });
    },
    onSuccess: (thread) => {
      queryClient.invalidateQueries({ queryKey: crmKeys.emailThreads(wsId) });
      queryClient.invalidateQueries({ queryKey: crmKeys.emailThread(wsId, thread.id) });
    },
  });

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Mail className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.emails.title)}</h1>
          {!isLoading && <span className="text-xs text-muted-foreground tabular-nums">{threads.length}</span>}
        </div>
      </PageHeader>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-4 p-5 lg:grid-cols-[360px_minmax(0,1fr)]">
        <aside className="flex min-h-0 flex-col gap-3">
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
            <Input className="pl-8" placeholder={t(($) => $.emails.search_placeholder)} value={search} onChange={(event) => setSearch(event.target.value)} />
          </div>
          {isLoading ? (
            <section className="space-y-2 rounded-lg border bg-card p-4">
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
            </section>
          ) : filteredThreads.length === 0 ? (
            <section className="rounded-lg border bg-card p-10 text-center">
              <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary">
                <Mail className="size-5" />
              </div>
              <h2 className="mt-4 text-base font-semibold">{t(($) => $.emails.empty_title)}</h2>
              <p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">
                {t(($) => $.emails.empty_description)}
              </p>
            </section>
          ) : (
            <section className="min-h-0 overflow-y-auto rounded-lg border bg-card">
              {filteredThreads.map((thread) => (
                <button key={thread.id} type="button" className={`block w-full border-b px-4 py-3 text-left text-sm last:border-b-0 hover:bg-accent ${selectedThread?.id === thread.id ? "bg-accent" : ""}`} onClick={() => {
                  setSelectedThreadId(thread.id);
                  setSelectedAccountId(thread.account_id ?? "");
                  setSelectedContactId(thread.contact_id ?? "");
                }}>
                  <div className="font-medium">{thread.subject}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {[thread.mailbox, thread.direction, thread.status, t(($) => $.common.count_messages, { count: thread.message_count })].filter(Boolean).join(" · ")}
                  </div>
                </button>
              ))}
            </section>
          )}
        </aside>

        <section className="min-h-0 overflow-hidden rounded-lg border bg-card">
          {!selectedThread ? (
            <div className="p-10 text-center text-sm text-muted-foreground">{t(($) => $.emails.select_thread)}</div>
          ) : (
            <div className="flex h-full min-h-0 flex-col">
              <div className="border-b p-5">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div>
                    <h2 className="text-base font-semibold">{selectedThread.subject}</h2>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {[selectedThread.mailbox, selectedThread.direction, selectedThread.status, messageTime(selectedThread.last_message_at)].filter(Boolean).join(" · ")}
                    </p>
                  </div>
                  {selectedAccount && (
                    <Button variant="outline" size="sm" onClick={() => navigation.push(paths.crmCustomerDetail(selectedAccount.id))}>
                      {t(($) => $.emails.open_customer)} <ArrowRight className="ml-1 size-3" />
                    </Button>
                  )}
                </div>
                <div className="mt-4 grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                  <select aria-label={t(($) => $.emails.linked_customer)} className="h-9 rounded-md border bg-background px-3 text-sm" value={effectiveAccountId} onChange={(event) => {
                    setSelectedAccountId(event.target.value);
                    setSelectedContactId("");
                  }}>
                    <option value="">{t(($) => $.emails.no_customer)}</option>
                    {accounts.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}
                  </select>
                  <select aria-label={t(($) => $.emails.linked_contact)} className="h-9 rounded-md border bg-background px-3 text-sm" value={selectedContactId || selectedThread.contact_id || ""} onChange={(event) => setSelectedContactId(event.target.value)} disabled={!effectiveAccountId}>
                    <option value="">{t(($) => $.emails.no_contact)}</option>
                    {contacts.map((contact) => <option key={contact.id} value={contact.id}>{contact.name}</option>)}
                  </select>
                  <Button size="sm" onClick={() => updateAssociation.mutate()} disabled={updateAssociation.isPending}>{t(($) => $.emails.save_association)}</Button>
                </div>
                {updateAssociation.isError && <p className="mt-2 text-xs text-destructive">{t(($) => $.emails.association_error)}</p>}
                {(selectedAccount || selectedContact) && (
                  <p className="mt-2 text-xs text-muted-foreground">
                    {[selectedAccount?.name, selectedContact?.name].filter(Boolean).join(" · ")}
                  </p>
                )}
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto p-5">
                {messagesLoading ? (
                  <div className="space-y-3">
                    <Skeleton className="h-24 w-full" />
                    <Skeleton className="h-24 w-full" />
                  </div>
                ) : messages.length === 0 ? (
                  <div className="rounded-lg border border-dashed p-8 text-center text-sm text-muted-foreground">{t(($) => $.emails.no_messages)}</div>
                ) : (
                  <div className="space-y-3">
                    {messages.map((message) => (
                      <article key={message.id} className="rounded-lg border p-4 text-sm">
                        <div className="flex flex-wrap justify-between gap-2">
                          <div className="font-medium">{message.from_name || message.from_email || t(($) => $.common.not_available)}</div>
                          <div className="text-xs text-muted-foreground">{messageTime(message.sent_at || message.received_at)}</div>
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {message.to_emails.length > 0 ? `To: ${message.to_emails.join(", ")}` : message.direction}
                        </div>
                        <div className="mt-3 whitespace-pre-wrap leading-6 text-muted-foreground">{message.body_text || message.snippet || t(($) => $.emails.no_body)}</div>
                      </article>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </section>
      </div>
    </div>
  );
}
