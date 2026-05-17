"use client";

import { useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { BarChart3, Flame, Mail, Plus, TrendingUp, Users } from "lucide-react";
import { crmAccountListOptions, crmEmailThreadListOptions } from "@multica/core/crm/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { CRMAccount, CRMAccountFollowUpBucket, CRMAccountPriority, CRMAccountRating, CRMAccountStatus } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";
import { useNavigation } from "../../navigation";
import { countryByCode, localizedName, localizedSort, normalizeLocale } from "../geo";
import { CRM_INDUSTRY_OPTIONS, industryLabel } from "../options";

function formatDate(value?: string | null) {
  return value ? new Date(value).toLocaleDateString() : "—";
}

type ReportFilter = Partial<Record<"status" | "rating" | "priority" | "country" | "industry" | "follow_up", string>>;
type ReportBucket = { key: string; label: string; value: number; filter: ReportFilter };

const statusOrder: CRMAccountStatus[] = ["prospect", "active", "inactive", "archived"];
const ratingOrder: CRMAccountRating[] = ["hot", "warm", "cold", "unknown"];
const priorityOrder: CRMAccountPriority[] = ["high", "medium", "low"];
const followUpOrder: CRMAccountFollowUpBucket[] = ["overdue", "today", "next_7_days", "none"];

function countBy<T extends string>(accounts: CRMAccount[], pick: (account: CRMAccount) => T | null | undefined) {
  return accounts.reduce((map, account) => {
    const key = pick(account);
    if (key) map.set(key, (map.get(key) ?? 0) + 1);
    return map;
  }, new Map<T, number>());
}

function followUpBucket(account: CRMAccount, now = new Date()): CRMAccountFollowUpBucket {
  if (!account.next_follow_up_at) return "none";
  const due = new Date(account.next_follow_up_at);
  if (Number.isNaN(due.getTime())) return "none";
  const todayEnd = new Date(now);
  todayEnd.setHours(23, 59, 59, 999);
  if (due < now) return "overdue";
  if (due <= todayEnd) return "today";
  const next7 = new Date(now);
  next7.setDate(next7.getDate() + 7);
  return due <= next7 ? "next_7_days" : "none";
}

function maxBucketValue(buckets: ReportBucket[]) {
  return Math.max(1, ...buckets.map((bucket) => bucket.value));
}

function ReportPanel({
  title,
  buckets,
  loading,
  onSelect,
  empty,
  icon,
}: {
  title: string;
  buckets: ReportBucket[];
  loading: boolean;
  onSelect: (filter: ReportFilter) => void;
  empty?: string;
  icon?: ReactNode;
}) {
  const max = maxBucketValue(buckets);
  return (
    <section className="rounded-lg border bg-card p-4">
      <div className="flex items-center justify-between text-sm font-medium">
        <h2>{title}</h2>
        {icon ? <span className="text-muted-foreground">{icon}</span> : null}
      </div>
      <div className="mt-3 space-y-2">
        {loading ? <Skeleton className="h-28" /> : buckets.length === 0 ? <p className="text-sm text-muted-foreground">{empty}</p> : buckets.map((bucket) => (
          <button key={bucket.key} type="button" className="grid w-full grid-cols-[minmax(90px,1fr)_3fr_3ch] items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs hover:bg-muted/50" onClick={() => onSelect(bucket.filter)}>
            <span className="truncate text-muted-foreground">{bucket.label}</span>
            <span className="h-2 rounded-full bg-muted"><span className="block h-2 rounded-full bg-primary/70" style={{ width: `${Math.max(5, (bucket.value / max) * 100)}%` }} /></span>
            <span className="text-right font-medium tabular-nums">{bucket.value}</span>
          </button>
        ))}
      </div>
    </section>
  );
}

export function CRMDashboardPage() {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const { t, i18n } = useT("crm");
  const locale = normalizeLocale(i18n.language);
  const { data: todayFollowUps = [], isLoading: todayLoading } = useQuery(crmAccountListOptions(wsId, { follow_up_bucket: "today", sort: "next_follow_up" }));
  const { data: weekFollowUps = [], isLoading: weekLoading } = useQuery(crmAccountListOptions(wsId, { follow_up_bucket: "next_7_days", sort: "next_follow_up" }));
  const { data: overdueFollowUps = [], isLoading: overdueLoading } = useQuery(crmAccountListOptions(wsId, { follow_up_bucket: "overdue", sort: "next_follow_up" }));
  const { data: highPriorityAccounts = [], isLoading: highPriorityLoading } = useQuery(crmAccountListOptions(wsId, { priority: "high", sort: "priority_rating" }));
  const { data: hotAccounts = [], isLoading: hotLoading } = useQuery(crmAccountListOptions(wsId, { rating: "hot", sort: "priority_rating" }));
  const { data: recentAccounts = [], isLoading: recentLoading } = useQuery(crmAccountListOptions(wsId, { sort: "updated" }));
  const { data: allAccounts = [], isLoading: reportsLoading } = useQuery(crmAccountListOptions(wsId, { sort: "name" }));
  const { data: emailThreads = [], isLoading: emailLoading } = useQuery(crmEmailThreadListOptions(wsId));
  const topTodayFollowUps = todayFollowUps.slice(0, 6);
  const topWeekFollowUps = weekFollowUps.slice(0, 6);
  const topOverdueFollowUps = overdueFollowUps.slice(0, 6);
  const topHighPriorityAccounts = highPriorityAccounts.slice(0, 6);
  const topHotAccounts = hotAccounts.slice(0, 6);
  const topRecentAccounts = recentAccounts.slice(0, 6);
  const topEmailThreads = emailThreads.slice(0, 6);
  const stats = useMemo(() => [
    { label: t(($) => $.dashboard.total_customers), value: allAccounts.length, icon: Users, filter: {} },
    { label: t(($) => $.dashboard.overdue_followups), value: overdueFollowUps.length, icon: Flame, filter: { follow_up: "overdue" } },
    { label: t(($) => $.dashboard.hot_customers), value: hotAccounts.length, icon: Flame, filter: { rating: "hot" } },
    { label: t(($) => $.dashboard.email_threads), value: emailThreads.length, icon: Mail, filter: null },
  ], [allAccounts.length, emailThreads.length, overdueFollowUps.length, hotAccounts.length, t]);

  const navigateToCustomers = (filter: ReportFilter = {}) => {
    const params = new URLSearchParams();
    Object.entries(filter).forEach(([key, value]) => {
      if (value) params.set(key, value);
    });
    navigation.push(`${paths.crmCustomers()}${params.size ? `?${params.toString()}` : ""}`);
  };

  const reportGroups = useMemo(() => {
    const statuses = countBy(allAccounts, (account) => account.status);
    const ratings = countBy(allAccounts, (account) => account.rating);
    const priorities = countBy(allAccounts, (account) => account.priority);
    const followUpsByBucket = countBy(allAccounts, (account) => followUpBucket(account));
    const countries = countBy(allAccounts, (account) => account.country_code || account.country_name || account.country);
    const industries = countBy(allAccounts, (account) => account.industry);

    return {
      funnel: ratingOrder.map((key) => ({ key, label: t(($) => $.ratings[key]), value: ratings.get(key) ?? 0, filter: { rating: key } })),
      status: statusOrder.map((key) => ({ key, label: t(($) => $.statuses[key]), value: statuses.get(key) ?? 0, filter: { status: key } })),
      priority: priorityOrder.map((key) => ({ key, label: t(($) => $.priorities[key]), value: priorities.get(key) ?? 0, filter: { priority: key } })),
      followUps: followUpOrder.map((key) => ({ key, label: t(($) => $.filters[`follow_up_${key}`]), value: followUpsByBucket.get(key) ?? 0, filter: { follow_up: key } })),
      countries: [...countries.entries()]
        .map(([key, value]) => ({ key, label: countryByCode(key) ? localizedName(countryByCode(key)!.name, locale) : key, value, filter: { country: key } }))
        .sort((a, b) => a.label.localeCompare(b.label, locale === "zh-Hans" ? "zh-Hans-CN-u-co-pinyin" : "en"))
        .slice(0, 8),
      industries: localizedSort(
        [...industries.entries()]
          .map(([key, value]) => ({ key, name: { en: industryLabel(key, "en"), zh: industryLabel(key, "zh-Hans") }, value, filter: { industry: key } })),
        locale,
      ).map((item) => ({ key: item.key, label: locale === "zh-Hans" ? item.name.zh : item.name.en, value: item.value, filter: item.filter })).slice(0, 8),
    };
  }, [allAccounts, locale, t]);

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
          {stats.map(({ label, value, icon: Icon, filter }) => (
            <button key={label} type="button" className="rounded-lg border bg-card p-4 text-left transition hover:border-primary/40 hover:bg-muted/30" onClick={() => filter ? navigateToCustomers(filter) : navigation.push(paths.crmEmails())}>
              <div className="flex items-center justify-between text-xs text-muted-foreground"><span>{label}</span><Icon className="size-4" /></div>
              <div className="mt-2 text-2xl font-semibold tabular-nums">{value}</div>
            </button>
          ))}
        </div>

        <section className="rounded-lg border bg-card p-4">
          <div className="flex items-center justify-between gap-3">
            <div>
              <h2 className="text-sm font-medium">{t(($) => $.dashboard.pipeline_title)}</h2>
              <p className="mt-1 text-xs text-muted-foreground">{t(($) => $.dashboard.pipeline_help)}</p>
            </div>
            <TrendingUp className="size-4 text-muted-foreground" />
          </div>
          {reportsLoading ? <Skeleton className="mt-4 h-36" /> : (
            <div className="mt-4 grid gap-3 lg:grid-cols-4">
              {reportGroups.funnel.map((bucket, index) => {
                const width = `${Math.max(8, (bucket.value / maxBucketValue(reportGroups.funnel)) * 100)}%`;
                return (
                  <button key={bucket.key} type="button" className="rounded-lg border p-3 text-left hover:bg-muted/40" onClick={() => navigateToCustomers(bucket.filter)}>
                    <div className="flex items-center justify-between text-sm"><span>{bucket.label}</span><span className="font-semibold tabular-nums">{bucket.value}</span></div>
                    <div className="mt-3 h-2 rounded-full bg-muted"><div className="h-2 rounded-full bg-primary" style={{ width, opacity: 1 - index * 0.14 }} /></div>
                  </button>
                );
              })}
            </div>
          )}
        </section>

        <div className="grid gap-4 xl:grid-cols-3">
          <ReportPanel title={t(($) => $.dashboard.status_distribution)} icon={<BarChart3 className="size-4" />} buckets={reportGroups.status} loading={reportsLoading} onSelect={navigateToCustomers} />
          <ReportPanel title={t(($) => $.dashboard.priority_distribution)} buckets={reportGroups.priority} loading={reportsLoading} onSelect={navigateToCustomers} />
          <ReportPanel title={t(($) => $.dashboard.overdue_trend)} buckets={reportGroups.followUps} loading={reportsLoading} onSelect={navigateToCustomers} />
          <ReportPanel title={t(($) => $.dashboard.country_distribution)} buckets={reportGroups.countries} loading={reportsLoading} onSelect={navigateToCustomers} empty={t(($) => $.dashboard.no_report_data)} />
          <ReportPanel title={t(($) => $.dashboard.industry_distribution)} buckets={reportGroups.industries} loading={reportsLoading} onSelect={navigateToCustomers} empty={t(($) => $.dashboard.no_report_data)} />
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-medium">{t(($) => $.dashboard.today_title)}</h2>
            <div className="mt-3">{todayLoading ? <Skeleton className="h-24" /> : accountList(topTodayFollowUps, t(($) => $.dashboard.no_today))}</div>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-medium">{t(($) => $.dashboard.week_title)}</h2>
            <div className="mt-3">{weekLoading ? <Skeleton className="h-24" /> : accountList(topWeekFollowUps, t(($) => $.dashboard.no_week))}</div>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-medium">{t(($) => $.dashboard.overdue_title)}</h2>
            <div className="mt-3">{overdueLoading ? <Skeleton className="h-24" /> : accountList(topOverdueFollowUps, t(($) => $.dashboard.no_followups))}</div>
          </section>
          <section className="rounded-lg border bg-card p-4">
            <h2 className="text-sm font-medium">{t(($) => $.dashboard.high_priority_title)}</h2>
            <div className="mt-3">{highPriorityLoading ? <Skeleton className="h-24" /> : accountList(topHighPriorityAccounts, t(($) => $.dashboard.no_high_priority))}</div>
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
