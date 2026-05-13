"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Flame, Mail, Plus, Users } from "lucide-react";
import { crmAccountListOptions, crmEmailThreadListOptions } from "@multica/core/crm/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";

function formatDate(value?: string | null) {
  return value ? new Date(value).toLocaleDateString() : "—";
}

export function CRMDashboardPage() {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { t } = useT("crm");
  const { data: followUps = [], isLoading: followUpsLoading } = useQuery(crmAccountListOptions(wsId, { follow_up_bucket: "overdue", sort: "next_follow_up" }));
  const { data: hotAccounts = [], isLoading: hotLoading } = useQuery(crmAccountListOptions(wsId, { rating: "hot", sort: "priority_rating" }));
  const { data: recentAccounts = [], isLoading: recentLoading } = useQuery(crmAccountListOptions(wsId, { sort: "updated" }));
  const { data: emailThreads = [], isLoading: emailLoading } = useQuery(crmEmailThreadListOptions(wsId));
  const topFollowUps = followUps.slice(0, 6);
  const topHotAccounts = hotAccounts.slice(0, 6);
  const topRecentAccounts = recentAccounts.slice(0, 6);
  const topEmailThreads = emailThreads.slice(0, 6);
  const stats = useMemo(() => [
    { label: t(($) => $.dashboard.total_customers), value: recentAccounts.length, icon: Users },
    { label: t(($) => $.dashboard.overdue_followups), value: followUps.length, icon: Flame },
    { label: t(($) => $.dashboard.hot_customers), value: hotAccounts.length, icon: Flame },
    { label: t(($) => $.dashboard.email_threads), value: emailThreads.length, icon: Mail },
  ], [emailThreads.length, followUps.length, hotAccounts.length, recentAccounts.length, t]);

  const accountList = (items: typeof recentAccounts, empty: string) => {
    if (items.length === 0) return <p className="text-sm text-muted-foreground">{empty}</p>;
    return <div className="space-y-2">{items.map((account) => (
      <button key={account.id} type="button" className="flex w-full items-center justify-between rounded-md border p-2 text-left text-sm hover:bg-muted/50" onClick={() => navigation.push(paths.crmCustomerDetail(account.id))}>
        <span className="truncate font-medium">{account.name}</span>
        <span className="ml-2 shrink-0 text-xs text-muted-foreground">{formatDate(account.next_follow_up_at || account.updated_at)}</span>
      </button>
    ))}</div>;
  };

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Users className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.dashboard.title)}</h1>
        </div>
        <div className="flex gap-2">
          <Button size="sm" variant="outline" onClick={() => navigation.push(paths.crmEmails())}>{t(($) => $.tabs.emails)}</Button>
          <Button size="sm" onClick={() => navigation.push(paths.crmCustomers())}><Plus className="mr-1 size-4" />{t(($) => $.customers.title)}</Button>
        </div>
      </PageHeader>
      <div className="space-y-4 p-5">
        <div className="grid gap-3 md:grid-cols-4">
          {stats.map(({ label, value, icon: Icon }) => (
            <div key={label} className="rounded-lg border bg-card p-4">
              <div className="flex items-center justify-between text-xs text-muted-foreground"><span>{label}</span><Icon className="size-4" /></div>
              <div className="mt-2 text-2xl font-semibold tabular-nums">{value}</div>
            </div>
          ))}
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-medium">{t(($) => $.dashboard.followups_title)}</h2>
            <div className="mt-3">{followUpsLoading ? <Skeleton className="h-24" /> : accountList(topFollowUps, t(($) => $.dashboard.no_followups))}</div>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-medium">{t(($) => $.dashboard.hot_title)}</h2>
            <div className="mt-3">{hotLoading ? <Skeleton className="h-24" /> : accountList(topHotAccounts, t(($) => $.dashboard.no_hot))}</div>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-medium">{t(($) => $.dashboard.recent_customers_title)}</h2>
            <div className="mt-3">{recentLoading ? <Skeleton className="h-24" /> : accountList(topRecentAccounts, t(($) => $.dashboard.no_customers))}</div>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-medium">{t(($) => $.dashboard.recent_emails_title)}</h2>
            <div className="mt-3">
              {emailLoading ? <Skeleton className="h-24" /> : topEmailThreads.length === 0 ? <p className="text-sm text-muted-foreground">{t(($) => $.dashboard.no_emails)}</p> : (
                <div className="space-y-2">{topEmailThreads.map((thread) => (
                  <button key={thread.id} type="button" className="flex w-full items-center justify-between rounded-md border p-2 text-left text-sm hover:bg-muted/50" onClick={() => navigation.push(paths.crmEmails())}>
                    <span className="truncate font-medium">{thread.subject || t(($) => $.notes.untitled)}</span>
                    <span className="ml-2 shrink-0 text-xs text-muted-foreground">{formatDate(thread.last_message_at || thread.updated_at)}</span>
                  </button>
                ))}</div>
              )}
            </div>
          </section>
        </div>
      </div>
    </div>
  );
}
