"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ArrowRight, Building2, Link2, Mail, Search, Send, Settings, UserRound } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { crmAccountListOptions, crmContactListOptions, crmEmailMessageListOptions, crmEmailThreadListOptions, crmKeys } from "@multica/core/crm/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import type { CRMAccount, CRMContact, CRMEmailThread, CreateCRMContactRequest } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

type AssociationDraft = {
  accountId: string;
  contactId: string;
  contactName: string;
  contactEmail: string;
};

function messageTime(value?: string | null) {
  return value ? new Date(value).toLocaleString() : "—";
}

function inferContactDraft(messages: Array<{ from_name?: string | null; from_email?: string | null; direction: string }>): Pick<AssociationDraft, "contactName" | "contactEmail"> {
  const inbound = messages.find((message) => message.direction === "inbound" && (message.from_name || message.from_email));
  const email = inbound?.from_email ?? "";
  const name = inbound?.from_name || email.split("@")[0] || "";
  return { contactName: name, contactEmail: email };
}

function DetailRow({ label, value }: { label: string; value?: string | null }) {
  return (
    <div className="rounded-md border bg-muted/20 px-3 py-2">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 truncate text-sm">{value || "—"}</div>
    </div>
  );
}

function AssociationChip({ icon, label, value, onClick }: { icon: ReactNode; label: string; value?: string | null; onClick?: () => void }) {
  return (
    <button
      type="button"
      className="group inline-flex min-w-0 items-center gap-2 rounded-full border bg-background px-3 py-1.5 text-left text-sm hover:bg-muted/60 disabled:pointer-events-none disabled:opacity-70"
      onClick={onClick}
      disabled={!onClick}
    >
      <span className="shrink-0 text-muted-foreground">{icon}</span>
      <span className="min-w-0">
        <span className="block text-[11px] leading-none text-muted-foreground">{label}</span>
        <span className="block truncate font-medium">{value || "—"}</span>
      </span>
    </button>
  );
}

export function CRMEmailsPage() {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const { t } = useT("crm");
  const [search, setSearch] = useState("");
  const [selectedThreadId, setSelectedThreadId] = useState<string | null>(null);
  const [detailDialog, setDetailDialog] = useState<{ type: "account"; account: CRMAccount } | { type: "contact"; contact: CRMContact } | null>(null);
  const [associationDraft, setAssociationDraft] = useState<AssociationDraft | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [mailboxSettings, setMailboxSettings] = useState({ host: "", port: 993, username: "", mailbox: "INBOX", use_tls: true, import_days: 30, bind_member_id: "", bind_agent_id: "" });
  const [compose, setCompose] = useState({ to: "", subject: "", body: "" });
  const { data: threads = [], isLoading } = useQuery(crmEmailThreadListOptions(wsId));
  const { data: accounts = [] } = useQuery(crmAccountListOptions(wsId, { sort: "name" }));
  const { data: members = [] } = useQuery({ queryKey: ["workspace-members", wsId], queryFn: () => api.listMembers(wsId), enabled: Boolean(wsId) });
  const { data: agents = [] } = useQuery({ queryKey: ["workspace-agents", wsId], queryFn: () => api.listAgents({ workspace_id: wsId }), enabled: Boolean(wsId) });

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

  const linkedAccountId = selectedThread?.account_id ?? "";
  const { data: contacts = [] } = useQuery({
    ...crmContactListOptions(wsId, linkedAccountId),
    enabled: Boolean(linkedAccountId),
  });
  const draftAccountId = associationDraft?.accountId ?? "";
  const { data: draftAccountContacts = [] } = useQuery({
    ...crmContactListOptions(wsId, draftAccountId),
    enabled: Boolean(draftAccountId),
  });
  const { data: messages = [], isLoading: messagesLoading } = useQuery({
    ...crmEmailMessageListOptions(wsId, selectedThread?.id ?? ""),
    enabled: Boolean(selectedThread?.id),
  });

  const selectedAccount = accounts.find((account) => account.id === linkedAccountId) ?? null;
  const selectedContact = contacts.find((contact) => contact.id === (selectedThread?.contact_id ?? "")) ?? null;

  const openAssociationDialog = () => {
    const inferred = inferContactDraft(messages);
    setAssociationDraft({
      accountId: selectedThread?.account_id ?? "",
      contactId: selectedThread?.contact_id ?? "",
      contactName: selectedContact?.name ?? inferred.contactName,
      contactEmail: selectedContact?.email ?? inferred.contactEmail,
    });
  };

  const updateAssociation = useMutation({
    mutationFn: async () => {
      if (!selectedThread || !associationDraft) throw new Error("No email association draft selected");
      let contactId = associationDraft.contactId || null;
      if (!contactId && associationDraft.accountId && associationDraft.contactName.trim()) {
        const payload: CreateCRMContactRequest = {
          account_id: associationDraft.accountId,
          name: associationDraft.contactName.trim(),
          email: associationDraft.contactEmail.trim() || null,
          is_primary: false,
        };
        const contact = await api.createCRMContact(associationDraft.accountId, payload);
        contactId = contact.id;
      }
      return api.updateCRMEmailThreadAssociation(selectedThread.id, {
        account_id: associationDraft.accountId || null,
        contact_id: contactId,
      });
    },
    onSuccess: async (thread) => {
      setAssociationDraft(null);
      await queryClient.invalidateQueries({ queryKey: crmKeys.emailThreads(wsId) });
      await queryClient.invalidateQueries({ queryKey: crmKeys.emailThread(wsId, thread.id) });
      if (thread.account_id) await queryClient.invalidateQueries({ queryKey: crmKeys.contacts(wsId, thread.account_id) });
    },
  });

  const saveAndImport = useMutation({
    mutationFn: () => api.saveAndImportCRMIMAPSettings({ ...mailboxSettings, enabled: true }),
    onSuccess: async () => {
      setSettingsOpen(false);
      await queryClient.invalidateQueries({ queryKey: crmKeys.emailThreads(wsId) });
    },
  });

  const saveDraft = useMutation({
    mutationFn: () => {
      if (!selectedThread) throw new Error("No thread selected");
      return api.createCRMEmailDraft(selectedThread.id, { to_emails: compose.to.split(",").map((x) => x.trim()).filter(Boolean), subject: compose.subject, body_text: compose.body });
    },
    onSuccess: async () => queryClient.invalidateQueries({ queryKey: crmKeys.emailMessages(wsId, selectedThread?.id ?? "") }),
  });

  const sendDraft = useMutation({
    mutationFn: () => {
      if (!selectedThread) throw new Error("No thread selected");
      return api.sendCRMEmailDraft(selectedThread.id, { to_emails: compose.to.split(",").map((x) => x.trim()).filter(Boolean), subject: compose.subject, body_text: compose.body });
    },
    onSuccess: async () => {
      setCompose({ to: "", subject: "", body: "" });
      await queryClient.invalidateQueries({ queryKey: crmKeys.emailMessages(wsId, selectedThread?.id ?? "") });
    },
  });

  const draftContacts = associationDraft?.accountId ? draftAccountContacts : [];

  return (
    <div className="flex h-full flex-col bg-muted/20">
      <PageHeader className="justify-between border-b bg-background px-5">
        <div className="flex items-center gap-2">
          <Mail className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.emails.title)}</h1>
          {!isLoading && <Badge variant="secondary" className="tabular-nums">{threads.length}</Badge>}
        </div>
        <Button variant="outline" size="sm" onClick={() => setSettingsOpen(true)}>
          <Settings className="mr-1 size-3" />
          邮箱设置
        </Button>
      </PageHeader>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-0 lg:grid-cols-[340px_minmax(0,1fr)]">
        <aside className="flex min-h-0 flex-col border-r bg-background">
          <div className="border-b p-3">
            <div className="relative">
              <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
              <Input className="pl-8" placeholder={t(($) => $.emails.search_placeholder)} value={search} onChange={(event) => setSearch(event.target.value)} />
            </div>
          </div>
          {isLoading ? (
            <section className="space-y-2 p-3">
              <Skeleton className="h-16 w-full" />
              <Skeleton className="h-16 w-full" />
            </section>
          ) : filteredThreads.length === 0 ? (
            <section className="m-3 rounded-lg border border-dashed bg-card p-10 text-center">
              <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary">
                <Mail className="size-5" />
              </div>
              <h2 className="mt-4 text-base font-semibold">{t(($) => $.emails.empty_title)}</h2>
              <p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">
                {t(($) => $.emails.empty_description)}
              </p>
            </section>
          ) : (
            <section className="min-h-0 flex-1 overflow-y-auto">
              {filteredThreads.map((thread) => {
                const active = selectedThread?.id === thread.id;
                return (
                  <button key={thread.id} type="button" className={`block w-full border-b px-4 py-3 text-left text-sm hover:bg-muted/60 ${active ? "bg-muted" : ""}`} onClick={() => setSelectedThreadId(thread.id)}>
                    <div className="flex items-start justify-between gap-2">
                      <div className="min-w-0 flex-1 truncate font-medium">{thread.subject}</div>
                      {!thread.account_id && <Badge variant="outline">{t(($) => $.emails.unlinked_badge)}</Badge>}
                    </div>
                    <div className="mt-1 truncate text-xs text-muted-foreground">
                      {[thread.mailbox, thread.direction, thread.status, t(($) => $.common.count_messages, { count: thread.message_count })].filter(Boolean).join(" · ")}
                    </div>
                  </button>
                );
              })}
            </section>
          )}
        </aside>

        <section className="min-h-0 overflow-hidden bg-background">
          {!selectedThread ? (
            <div className="p-10 text-center text-sm text-muted-foreground">{t(($) => $.emails.select_thread)}</div>
          ) : (
            <div className="flex h-full min-h-0 flex-col">
              <div className="border-b bg-background p-5">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <h2 className="truncate text-base font-semibold">{selectedThread.subject}</h2>
                    <p className="mt-1 text-xs text-muted-foreground">
                      {[selectedThread.mailbox, selectedThread.direction, selectedThread.status, messageTime(selectedThread.last_message_at)].filter(Boolean).join(" · ")}
                    </p>
                  </div>
                  <Button variant={selectedAccount ? "outline" : "default"} size="sm" onClick={openAssociationDialog}>
                    <Link2 className="mr-1 size-3" />
                    {selectedAccount ? t(($) => $.emails.change_association) : t(($) => $.emails.link_customer_contact)}
                  </Button>
                </div>
                <div className="mt-4 flex flex-wrap items-center gap-2">
                  <AssociationChip icon={<Building2 className="size-4" />} label={t(($) => $.emails.linked_customer)} value={selectedAccount?.name ?? t(($) => $.emails.no_customer)} onClick={selectedAccount ? () => setDetailDialog({ type: "account", account: selectedAccount }) : undefined} />
                  <AssociationChip icon={<UserRound className="size-4" />} label={t(($) => $.emails.linked_contact)} value={selectedContact?.name ?? t(($) => $.emails.no_contact)} onClick={selectedContact ? () => setDetailDialog({ type: "contact", contact: selectedContact }) : undefined} />
                  {selectedAccount && (
                    <Button variant="ghost" size="sm" onClick={() => navigation.push(paths.crmCustomerDetail(selectedAccount.id))}>
                      {t(($) => $.emails.open_customer)} <ArrowRight className="ml-1 size-3" />
                    </Button>
                  )}
                </div>
                {updateAssociation.isError && <p className="mt-2 text-xs text-destructive">{t(($) => $.emails.association_error)}</p>}
              </div>
              <div className="min-h-0 flex-1 overflow-y-auto bg-muted/20 p-5">
                <div className="mb-4 rounded-lg border bg-background p-4 text-sm shadow-xs">
                  <div className="mb-2 flex items-center gap-2 font-medium"><Send className="size-4" />撰写草稿</div>
                  <div className="grid gap-2 sm:grid-cols-2">
                    <Input placeholder="To, comma separated" value={compose.to} onChange={(event) => setCompose({ ...compose, to: event.target.value })} />
                    <Input placeholder="Subject" value={compose.subject} onChange={(event) => setCompose({ ...compose, subject: event.target.value })} />
                  </div>
                  <textarea className="mt-2 min-h-24 w-full rounded-md border bg-background px-3 py-2 text-sm" placeholder="Body" value={compose.body} onChange={(event) => setCompose({ ...compose, body: event.target.value })} />
                  <div className="mt-2 flex justify-end gap-2">
                    <Button variant="outline" size="sm" disabled={saveDraft.isPending || !compose.to || !compose.subject} onClick={() => saveDraft.mutate()}>保存草稿</Button>
                    <Button size="sm" disabled={sendDraft.isPending || !compose.to || !compose.subject} onClick={() => sendDraft.mutate()}>发送</Button>
                  </div>
                </div>
                {messagesLoading ? (
                  <div className="space-y-3">
                    <Skeleton className="h-24 w-full" />
                    <Skeleton className="h-24 w-full" />
                  </div>
                ) : messages.length === 0 ? (
                  <div className="rounded-lg border border-dashed bg-background p-8 text-center text-sm text-muted-foreground">{t(($) => $.emails.no_messages)}</div>
                ) : (
                  <div className="space-y-3">
                    {messages.map((message) => (
                      <article key={message.id} className="rounded-lg border bg-background p-4 text-sm shadow-xs">
                        <div className="flex flex-wrap justify-between gap-2">
                          <div className="font-medium">{message.from_name || message.from_email || t(($) => $.common.not_available)}</div>
                          <div className="text-xs text-muted-foreground">{messageTime(message.sent_at || message.received_at)}</div>
                        </div>
                        <div className="mt-1 text-xs text-muted-foreground">
                          {message.to_emails.length > 0 ? `${t(($) => $.emails.to_label)}: ${message.to_emails.join(", ")}` : message.direction}
                        </div>
                        <div className="mt-3 whitespace-pre-wrap leading-6 text-foreground/80">{message.body_text || message.snippet || t(($) => $.emails.no_body)}</div>
                      </article>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
        </section>
      </div>

      <Dialog open={settingsOpen} onOpenChange={setSettingsOpen}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>邮箱设置</DialogTitle>
            <DialogDescription>保存 IMAP 设置并记录导入范围与成员/Agent 绑定。</DialogDescription>
          </DialogHeader>
          <div className="grid gap-3 sm:grid-cols-2">
            <Input placeholder="IMAP host" value={mailboxSettings.host} onChange={(event) => setMailboxSettings({ ...mailboxSettings, host: event.target.value })} />
            <Input type="number" placeholder="Port" value={mailboxSettings.port} onChange={(event) => setMailboxSettings({ ...mailboxSettings, port: Number(event.target.value) })} />
            <Input placeholder="Username" value={mailboxSettings.username} onChange={(event) => setMailboxSettings({ ...mailboxSettings, username: event.target.value })} />
            <Input placeholder="Mailbox" value={mailboxSettings.mailbox} onChange={(event) => setMailboxSettings({ ...mailboxSettings, mailbox: event.target.value })} />
            <label className="space-y-1 text-sm">
              <span className="text-xs font-medium text-muted-foreground">导入范围</span>
              <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={mailboxSettings.import_days} onChange={(event) => setMailboxSettings({ ...mailboxSettings, import_days: Number(event.target.value) })}>
                <option value={7}>最近 7 天</option>
                <option value={30}>最近 30 天</option>
                <option value={90}>最近 90 天</option>
              </select>
            </label>
            <label className="space-y-1 text-sm">
              <span className="text-xs font-medium text-muted-foreground">绑定成员</span>
              <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={mailboxSettings.bind_member_id} onChange={(event) => setMailboxSettings({ ...mailboxSettings, bind_member_id: event.target.value })}>
                <option value="">不绑定成员</option>
                {members.map((member) => <option key={member.id} value={member.id}>{member.name || member.email || member.id}</option>)}
              </select>
            </label>
            <label className="space-y-1 text-sm">
              <span className="text-xs font-medium text-muted-foreground">绑定智能体</span>
              <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={mailboxSettings.bind_agent_id} onChange={(event) => setMailboxSettings({ ...mailboxSettings, bind_agent_id: event.target.value })}>
                <option value="">不绑定智能体</option>
                {agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
              </select>
            </label>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={mailboxSettings.use_tls} onChange={(event) => setMailboxSettings({ ...mailboxSettings, use_tls: event.target.checked })} />TLS</label>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setSettingsOpen(false)}>取消</Button>
            <Button disabled={saveAndImport.isPending || !mailboxSettings.host || !mailboxSettings.username} onClick={() => saveAndImport.mutate()}>保存并导入</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={detailDialog !== null} onOpenChange={(open) => !open && setDetailDialog(null)}>
        <DialogContent className="sm:max-w-lg">
          {detailDialog?.type === "account" && (
            <>
              <DialogHeader>
                <DialogTitle>{detailDialog.account.name}</DialogTitle>
                <DialogDescription>{t(($) => $.emails.customer_detail)}</DialogDescription>
              </DialogHeader>
              <div className="grid gap-3 sm:grid-cols-2">
                <DetailRow label={t(($) => $.customers.status)} value={t(($) => $.statuses[detailDialog.account.status])} />
                <DetailRow label={t(($) => $.customers.rating)} value={t(($) => $.ratings[detailDialog.account.rating])} />
                <DetailRow label={t(($) => $.customers.priority)} value={t(($) => $.priorities[detailDialog.account.priority])} />
                <DetailRow label={t(($) => $.customers.country)} value={detailDialog.account.country_name || detailDialog.account.country} />
                <DetailRow label={t(($) => $.customers.website)} value={detailDialog.account.website} />
                <DetailRow label={t(($) => $.customers.next_follow_up_at)} value={messageTime(detailDialog.account.next_follow_up_at)} />
              </div>
              <DialogFooter>
                <Button variant="outline" onClick={() => setDetailDialog(null)}>{t(($) => $.actions.cancel)}</Button>
                <Button onClick={() => navigation.push(paths.crmCustomerDetail(detailDialog.account.id))}>{t(($) => $.emails.open_customer)}</Button>
              </DialogFooter>
            </>
          )}
          {detailDialog?.type === "contact" && (
            <>
              <DialogHeader>
                <DialogTitle>{detailDialog.contact.name}</DialogTitle>
                <DialogDescription>{t(($) => $.emails.contact_detail)}</DialogDescription>
              </DialogHeader>
              <div className="grid gap-3 sm:grid-cols-2">
                <DetailRow label={t(($) => $.contacts.email)} value={detailDialog.contact.email} />
                <DetailRow label={t(($) => $.contacts.phone)} value={detailDialog.contact.phone || detailDialog.contact.mobile} />
                <DetailRow label={t(($) => $.contacts.whatsapp)} value={detailDialog.contact.whatsapp || detailDialog.contact.whatsapp_id} />
                <DetailRow label={t(($) => $.contacts.job_title)} value={detailDialog.contact.job_title || detailDialog.contact.role_title} />
                <DetailRow label={t(($) => $.contacts.department)} value={detailDialog.contact.department} />
                <DetailRow label={t(($) => $.contacts.preferred_language)} value={detailDialog.contact.preferred_language || detailDialog.contact.language} />
              </div>
            </>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={associationDraft !== null} onOpenChange={(open) => !open && setAssociationDraft(null)}>
        <DialogContent className="sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{t(($) => $.emails.link_customer_contact)}</DialogTitle>
            <DialogDescription>{t(($) => $.emails.link_help)}</DialogDescription>
          </DialogHeader>
          {associationDraft && (
            <div className="space-y-4">
              <label className="space-y-1 text-sm">
                <span className="text-xs font-medium text-muted-foreground">{t(($) => $.emails.linked_customer)}</span>
                <select aria-label={t(($) => $.emails.linked_customer)} className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={associationDraft.accountId} onChange={(event) => setAssociationDraft({ ...associationDraft, accountId: event.target.value, contactId: "" })}>
                  <option value="">{t(($) => $.emails.no_customer)}</option>
                  {accounts.map((account) => <option key={account.id} value={account.id}>{account.name}</option>)}
                </select>
              </label>
              {associationDraft.accountId && draftContacts.length > 0 && (
                <label className="space-y-1 text-sm">
                  <span className="text-xs font-medium text-muted-foreground">{t(($) => $.emails.existing_contact)}</span>
                  <select aria-label={t(($) => $.emails.linked_contact)} className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={associationDraft.contactId} onChange={(event) => setAssociationDraft({ ...associationDraft, contactId: event.target.value })}>
                    <option value="">{t(($) => $.emails.create_new_contact)}</option>
                    {draftContacts.map((contact) => <option key={contact.id} value={contact.id}>{contact.name}</option>)}
                  </select>
                </label>
              )}
              {associationDraft.accountId && !associationDraft.contactId && (
                <div className="grid gap-3 rounded-lg border bg-muted/20 p-3 sm:grid-cols-2">
                  <div className="sm:col-span-2 text-xs font-medium text-muted-foreground">{t(($) => $.emails.new_contact_title)}</div>
                  <Input aria-label={t(($) => $.contacts.name)} value={associationDraft.contactName} onChange={(event) => setAssociationDraft({ ...associationDraft, contactName: event.target.value })} placeholder={t(($) => $.contacts.name)} />
                  <Input aria-label={t(($) => $.contacts.email)} value={associationDraft.contactEmail} onChange={(event) => setAssociationDraft({ ...associationDraft, contactEmail: event.target.value })} placeholder={t(($) => $.contacts.email)} />
                </div>
              )}
            </div>
          )}
          {updateAssociation.isError && <p className="text-xs text-destructive">{t(($) => $.emails.association_error)}</p>}
          <DialogFooter>
            <Button variant="outline" onClick={() => setAssociationDraft(null)}>{t(($) => $.actions.cancel)}</Button>
            <Button disabled={!associationDraft?.accountId || updateAssociation.isPending} onClick={() => updateAssociation.mutate()}>{t(($) => $.emails.save_association)}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
