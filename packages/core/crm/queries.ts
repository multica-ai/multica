import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListCRMAccountsParams } from "../types";

export const crmKeys = {
  all: (wsId: string) => ["crm", wsId] as const,
  accounts: (wsId: string) => [...crmKeys.all(wsId), "accounts"] as const,
  accountList: (wsId: string, params: ListCRMAccountsParams = {}) =>
    [...crmKeys.accounts(wsId), "list", params] as const,
  accountDetail: (wsId: string, id: string) =>
    [...crmKeys.accounts(wsId), "detail", id] as const,
  contacts: (wsId: string, accountId: string) =>
    [...crmKeys.accountDetail(wsId, accountId), "contacts"] as const,
  profile: (wsId: string, accountId: string) =>
    [...crmKeys.accountDetail(wsId, accountId), "profile"] as const,
  notes: (wsId: string, accountId: string) =>
    [...crmKeys.accountDetail(wsId, accountId), "notes"] as const,
  emailThreads: (wsId: string, accountId = "") =>
    [...crmKeys.all(wsId), "email-threads", accountId] as const,
  emailThread: (wsId: string, threadId: string) =>
    [...crmKeys.all(wsId), "email-threads", threadId] as const,
  emailMessages: (wsId: string, threadId: string) =>
    [...crmKeys.emailThread(wsId, threadId), "messages"] as const,
  emailEngineStatus: (wsId: string, mailboxId = "") =>
    [...crmKeys.all(wsId), "emailengine", "status", mailboxId] as const,
  aiSettings: (wsId: string) => [...crmKeys.all(wsId), "ai-settings"] as const,
};

export function crmAccountListOptions(wsId: string, params: ListCRMAccountsParams = {}) {
  return queryOptions({
    queryKey: crmKeys.accountList(wsId, params),
    queryFn: () => api.listCRMAccounts(params),
    select: (data) => data.accounts,
  });
}

export function crmAccountDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: crmKeys.accountDetail(wsId, id),
    queryFn: () => api.getCRMAccount(id),
  });
}

export function crmContactListOptions(wsId: string, accountId: string) {
  return queryOptions({
    queryKey: crmKeys.contacts(wsId, accountId),
    queryFn: () => api.listCRMContacts(accountId),
    select: (data) => data.contacts,
  });
}

export function crmAccountProfileOptions(wsId: string, accountId: string) {
  return queryOptions({
    queryKey: crmKeys.profile(wsId, accountId),
    queryFn: () => api.getCRMAccountProfile(accountId),
  });
}

export function crmCommunicationNoteListOptions(wsId: string, accountId: string) {
  return queryOptions({
    queryKey: crmKeys.notes(wsId, accountId),
    queryFn: () => api.listCRMCommunicationNotes(accountId),
    select: (data) => data.notes,
  });
}

export function crmEmailThreadListOptions(wsId: string, accountId = "", folder = "", mailbox = "") {
  return queryOptions({
    queryKey: [...crmKeys.emailThreads(wsId, accountId), folder, mailbox],
    queryFn: () => api.listCRMEmailThreads({ account_id: accountId || undefined, folder: folder || undefined, mailbox: mailbox || undefined }),
    select: (data) => data.threads,
  });
}

export function crmEmailThreadDetailOptions(wsId: string, threadId: string) {
  return queryOptions({
    queryKey: crmKeys.emailThread(wsId, threadId),
    queryFn: () => api.getCRMEmailThread(threadId),
    enabled: Boolean(threadId),
  });
}

export function crmEmailMessageListOptions(wsId: string, threadId: string) {
  return queryOptions({
    queryKey: crmKeys.emailMessages(wsId, threadId),
    queryFn: () => api.listCRMEmailMessages(threadId),
    select: (data) => data.messages,
  });
}

export function crmEmailEngineStatusOptions(wsId: string, mailboxId = "") {
  return queryOptions({
    queryKey: crmKeys.emailEngineStatus(wsId, mailboxId),
    queryFn: () => api.getCRMEmailEngineStatus(mailboxId || undefined),
    enabled: Boolean(wsId),
  });
}
