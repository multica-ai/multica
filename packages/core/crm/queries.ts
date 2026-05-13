import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const crmKeys = {
  all: (wsId: string) => ["crm", wsId] as const,
  accounts: (wsId: string) => [...crmKeys.all(wsId), "accounts"] as const,
  accountList: (wsId: string, search = "") =>
    [...crmKeys.accounts(wsId), "list", search] as const,
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
  emailMessages: (wsId: string, threadId: string) =>
    [...crmKeys.all(wsId), "email-threads", threadId, "messages"] as const,
};

export function crmAccountListOptions(wsId: string, search = "") {
  return queryOptions({
    queryKey: crmKeys.accountList(wsId, search),
    queryFn: () => api.listCRMAccounts({ search }),
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

export function crmEmailThreadListOptions(wsId: string, accountId = "") {
  return queryOptions({
    queryKey: crmKeys.emailThreads(wsId, accountId),
    queryFn: () => api.listCRMEmailThreads(accountId ? { account_id: accountId } : undefined),
    select: (data) => data.threads,
  });
}

export function crmEmailMessageListOptions(wsId: string, threadId: string) {
  return queryOptions({
    queryKey: crmKeys.emailMessages(wsId, threadId),
    queryFn: () => api.listCRMEmailMessages(threadId),
    select: (data) => data.messages,
  });
}
