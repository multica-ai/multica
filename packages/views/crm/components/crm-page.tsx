"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Building2, Plus, Search } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { crmAccountListOptions, crmKeys } from "@multica/core/crm/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { useNavigation } from "../../navigation";
import type { CRMAccountPriority, CRMAccountRating, CRMAccountSource, CRMAccountStatus, CRMAccountType } from "@multica/core/types";
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { useT } from "../../i18n";
import type crmResources from "../../locales/en/crm.json";
import { PageHeader } from "../../layout/page-header";
import { COUNTRY_OPTIONS, countryByCode, loadCityOptions, loadRegionOptions, localizedName, localizedSort, normalizeLocale, useLocationSelection } from "../geo";
import { appendTag, CRM_INDUSTRY_OPTIONS, formatDateTimeLocal, industryLabel, optionLabel, splitTags, subIndustryOptions } from "../options";

type CRMResources = typeof crmResources;
type Translation = (
  selector: (resources: CRMResources) => string,
  options?: Record<string, unknown>,
) => string;

type AccountFormState = {
  name: string;
  accountType: CRMAccountType;
  status: CRMAccountStatus;
  source: CRMAccountSource;
  rating: CRMAccountRating;
  priority: CRMAccountPriority;
  website: string;
  countryCode: string;
  regionCode: string;
  cityCode: string;
  industry: string;
  subIndustry: string;
  annualRevenue: string;
  employeeCount: string;
  tags: string;
  nextFollowUpAt: string;
  notes: string;
};

const blankAccountForm = (): AccountFormState => ({
  name: "",
  accountType: "prospect",
  status: "active",
  source: "manual",
  rating: "unknown",
  priority: "medium",
  website: "",
  countryCode: "",
  regionCode: "",
  cityCode: "",
  industry: "",
  subIndustry: "",
  annualRevenue: "",
  employeeCount: "",
  tags: "",
  nextFollowUpAt: formatDateTimeLocal(),
  notes: "",
});

const tagSuggestions = (accounts: Array<{ tags?: string[] | null }>) => {
  const counts = new Map<string, number>();
  accounts.forEach((account) => {
    account.tags?.forEach((tag) => {
      const normalized = tag.trim();
      if (!normalized) return;
      counts.set(normalized, (counts.get(normalized) ?? 0) + 1);
    });
  });
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([tag]) => tag)
    .slice(0, 12);
};

function AccountStatusLabel({ status, t }: { status: CRMAccountStatus; t: Translation }) {
  const labels: Record<CRMAccountStatus, string> = {
    active: t(($) => $.statuses.active),
    inactive: t(($) => $.statuses.inactive),
    prospect: t(($) => $.statuses.prospect),
    archived: t(($) => $.statuses.archived),
  };
  return labels[status] ?? status;
}

function AccountTypeLabel({ type, t }: { type: CRMAccountType; t: Translation }) {
  const labels: Record<CRMAccountType, string> = {
    prospect: t(($) => $.account_types.prospect),
    customer: t(($) => $.account_types.customer),
    partner: t(($) => $.account_types.partner),
    supplier: t(($) => $.account_types.supplier),
    competitor: t(($) => $.account_types.competitor),
    other: t(($) => $.account_types.other),
  };
  return labels[type] ?? type;
}

function AccountForm({
  form,
  setForm,
  t,
  locale,
  suggestedTags,
}: {
  form: AccountFormState;
  setForm: (next: AccountFormState) => void;
  t: Translation;
  locale: "en" | "zh-Hans";
  suggestedTags: string[];
}) {
  const { regions, cities, regionsLoading, citiesLoading } = useLocationSelection(form.countryCode, form.regionCode, locale);
  const countries = useMemo(() => localizedSort(COUNTRY_OPTIONS, locale), [locale]);
  const subIndustries = subIndustryOptions(form.industry);

  return (
    <div className="grid max-h-[70vh] gap-3 overflow-y-auto pr-1 sm:grid-cols-2">
      <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder={t(($) => $.customers.new_customer_name)} />
      <Input value={form.website} onChange={(e) => setForm({ ...form, website: e.target.value })} placeholder={t(($) => $.customers.website)} />
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.accountType} onChange={(e) => setForm({ ...form, accountType: e.target.value as CRMAccountType })}>
        <option value="prospect">{t(($) => $.account_types.prospect)}</option>
        <option value="customer">{t(($) => $.account_types.customer)}</option>
        <option value="partner">{t(($) => $.account_types.partner)}</option>
        <option value="supplier">{t(($) => $.account_types.supplier)}</option>
        <option value="competitor">{t(($) => $.account_types.competitor)}</option>
        <option value="other">{t(($) => $.account_types.other)}</option>
      </select>
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.status} onChange={(e) => setForm({ ...form, status: e.target.value as CRMAccountStatus })}>
        <option value="active">{t(($) => $.statuses.active)}</option>
        <option value="prospect">{t(($) => $.statuses.prospect)}</option>
        <option value="inactive">{t(($) => $.statuses.inactive)}</option>
        <option value="archived">{t(($) => $.statuses.archived)}</option>
      </select>
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.source} onChange={(e) => setForm({ ...form, source: e.target.value as CRMAccountSource })}>
        <option value="manual">{t(($) => $.sources.manual)}</option>
        <option value="email">{t(($) => $.sources.email)}</option>
        <option value="whatsapp">{t(($) => $.sources.whatsapp)}</option>
        <option value="website">{t(($) => $.sources.website)}</option>
        <option value="referral">{t(($) => $.sources.referral)}</option>
        <option value="trade_show">{t(($) => $.sources.trade_show)}</option>
        <option value="linkedin">{t(($) => $.sources.linkedin)}</option>
        <option value="other">{t(($) => $.sources.other)}</option>
      </select>
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.rating} onChange={(e) => setForm({ ...form, rating: e.target.value as CRMAccountRating })}>
        <option value="unknown">{t(($) => $.ratings.unknown)}</option>
        <option value="hot">{t(($) => $.ratings.hot)}</option>
        <option value="warm">{t(($) => $.ratings.warm)}</option>
        <option value="cold">{t(($) => $.ratings.cold)}</option>
      </select>
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.priority} onChange={(e) => setForm({ ...form, priority: e.target.value as CRMAccountPriority })}>
        <option value="medium">{t(($) => $.priorities.medium)}</option>
        <option value="high">{t(($) => $.priorities.high)}</option>
        <option value="low">{t(($) => $.priorities.low)}</option>
      </select>
      <select aria-label={t(($) => $.customers.country)} className="h-9 rounded-md border bg-background px-3 text-sm" value={form.countryCode} onChange={(e) => setForm({ ...form, countryCode: e.target.value, regionCode: "", cityCode: "" })}>
        <option value="">{t(($) => $.customers.country)}</option>
        {countries.map((option) => <option key={option.code} value={option.code}>{localizedName(option.name, locale)}</option>)}
      </select>
      <select aria-label={t(($) => $.customers.region)} className="h-9 rounded-md border bg-background px-3 text-sm" value={form.regionCode} onChange={(e) => setForm({ ...form, regionCode: e.target.value, cityCode: "" })} disabled={!form.countryCode || regionsLoading}>
        <option value="">{regionsLoading ? `${t(($) => $.customers.region)}...` : t(($) => $.customers.region)}</option>
        {regions.map((option) => <option key={option.code} value={option.code}>{localizedName(option.name, locale)}</option>)}
      </select>
      <select aria-label={t(($) => $.customers.city)} className="h-9 rounded-md border bg-background px-3 text-sm" value={form.cityCode} onChange={(e) => setForm({ ...form, cityCode: e.target.value })} disabled={!form.regionCode || citiesLoading}>
        <option value="">{citiesLoading ? `${t(($) => $.customers.city)}...` : t(($) => $.customers.city)}</option>
        {cities.map((option) => <option key={option.code} value={option.code}>{localizedName(option.name, locale)}</option>)}
      </select>
      <select aria-label={t(($) => $.customers.industry)} className="h-9 rounded-md border bg-background px-3 text-sm" value={form.industry} onChange={(e) => setForm({ ...form, industry: e.target.value, subIndustry: "" })}>
        <option value="">{t(($) => $.customers.industry)}</option>
        {CRM_INDUSTRY_OPTIONS.map((option) => <option key={option.value} value={option.value}>{industryLabel(option.value, locale)}</option>)}
      </select>
      <select aria-label={t(($) => $.customers.sub_industry)} className="h-9 rounded-md border bg-background px-3 text-sm" value={form.subIndustry} onChange={(e) => setForm({ ...form, subIndustry: e.target.value })} disabled={!form.industry}>
        <option value="">{t(($) => $.customers.sub_industry)}</option>
        {subIndustries.map((option) => <option key={option.value} value={option.value}>{optionLabel(option, locale)}</option>)}
      </select>
      <Input value={form.annualRevenue} onChange={(e) => setForm({ ...form, annualRevenue: e.target.value })} placeholder={t(($) => $.customers.annual_revenue)} />
      <Input value={form.employeeCount} onChange={(e) => setForm({ ...form, employeeCount: e.target.value })} placeholder={t(($) => $.customers.employee_count)} />
      <div className="space-y-2 sm:col-span-2">
        <Input aria-label={t(($) => $.customers.tags)} value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} placeholder={t(($) => $.customers.tags_placeholder)} />
        {suggestedTags.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {suggestedTags.map((tag) => (
              <button key={tag} type="button" className="rounded-full border px-2 py-0.5 text-xs text-muted-foreground hover:bg-accent" onClick={() => setForm({ ...form, tags: appendTag(form.tags, tag) })}>{tag}</button>
            ))}
          </div>
        )}
      </div>
      <Input aria-label={t(($) => $.customers.next_follow_up_at)} className="sm:col-span-2" type="datetime-local" value={form.nextFollowUpAt} onChange={(e) => setForm({ ...form, nextFollowUpAt: e.target.value })} />
      <textarea className="min-h-24 rounded-md border bg-background px-3 py-2 text-sm sm:col-span-2" value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} placeholder={t(($) => $.customers.notes)} />
    </div>
  );
}

export function CRMPage() {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const { t: rawT, i18n } = useT("crm");
  const t = rawT as Translation;
  const locale = normalizeLocale(i18n.language);
  const [search, setSearch] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [form, setForm] = useState<AccountFormState>(() => blankAccountForm());

  const { data: accounts = [], isLoading } = useQuery(crmAccountListOptions(wsId, search));
  const sortedAccounts = useMemo(
    () => [...accounts].sort((a, b) => a.name.localeCompare(b.name)),
    [accounts],
  );
  const suggestedTags = useMemo(() => tagSuggestions(accounts), [accounts]);

  const createAccount = useMutation({
    mutationFn: async () => {
      const country = countryByCode(form.countryCode);
      const regions = await loadRegionOptions(form.countryCode, locale);
      const region = regions.find((option) => option.code === form.regionCode);
      const cities = await loadCityOptions(form.countryCode, form.regionCode, locale);
      const city = cities.find((option) => option.code === form.cityCode);
      return api.createCRMAccount({
        name: form.name,
        account_type: form.accountType,
        website: form.website || null,
        country: form.countryCode || null,
        country_code: form.countryCode || null,
        country_name: country ? localizedName(country.name, locale) : null,
        region: region ? localizedName(region.name, locale) : null,
        city: city ? localizedName(city.name, locale) : null,
        industry: form.industry || null,
        sub_industry: form.subIndustry || null,
        status: form.status,
        source: form.source,
        rating: form.rating,
        priority: form.priority,
        annual_revenue: form.annualRevenue || null,
        employee_count: form.employeeCount || null,
        tags: splitTags(form.tags),
        next_follow_up_at: form.nextFollowUpAt ? new Date(form.nextFollowUpAt).toISOString() : null,
        notes: form.notes || null,
      });
    },
    onSuccess: async (account) => {
      setForm(blankAccountForm());
      setCreateOpen(false);
      await queryClient.invalidateQueries({ queryKey: crmKeys.accounts(wsId) });
      navigation.push(paths.crmCustomerDetail(account.id));
    },
  });

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Building2 className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.customers.title)}</h1>
          {!isLoading && <span className="text-xs text-muted-foreground tabular-nums">{accounts.length}</span>}
        </div>
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          <Plus className="mr-1 size-4" /> {t(($) => $.customers.add_customer)}
        </Button>
      </PageHeader>

      <div className="space-y-4 p-5">
        <div className="relative max-w-md">
          <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
          <Input className="pl-8" value={search} onChange={(e) => setSearch(e.target.value)} placeholder={t(($) => $.customers.search_placeholder)} />
        </div>

        <section className="rounded-lg border bg-card">
          {isLoading ? (
            <div className="space-y-2 p-4">
              {Array.from({ length: 6 }).map((_, i) => <Skeleton key={i} className="h-12 w-full" />)}
            </div>
          ) : sortedAccounts.length === 0 ? (
            <div className="p-10 text-center text-sm text-muted-foreground">{t(($) => $.customers.empty)}</div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t(($) => $.customers.title)}</TableHead>
                  <TableHead>{t(($) => $.customers.account_type)}</TableHead>
                  <TableHead>{t(($) => $.customers.status)}</TableHead>
                  <TableHead>{t(($) => $.customers.country)}</TableHead>
                  <TableHead>{t(($) => $.customers.industry)}</TableHead>
                  <TableHead>{t(($) => $.tabs.contacts)}</TableHead>
                  <TableHead>{t(($) => $.customers.next_follow_up_at)}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedAccounts.map((account) => (
                  <TableRow
                    key={account.id}
                    className="cursor-pointer"
                    onClick={() => navigation.push(paths.crmCustomerDetail(account.id))}
                  >
                    <TableCell className="font-medium">{account.name}</TableCell>
                    <TableCell><AccountTypeLabel type={account.account_type} t={t} /></TableCell>
                    <TableCell><AccountStatusLabel status={account.status} t={t} /></TableCell>
                    <TableCell>{account.country_code ? localizedName(countryByCode(account.country_code)?.name ?? { en: account.country_name || account.country_code, zh: account.country_name || account.country_code }, locale) : account.country_name || account.country || "—"}</TableCell>
                    <TableCell>{[account.industry, account.sub_industry].filter(Boolean).join(" · ") || "—"}</TableCell>
                    <TableCell>{account.contact_count}</TableCell>
                    <TableCell>{account.next_follow_up_at ? new Date(account.next_follow_up_at).toLocaleString() : "—"}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </section>
      </div>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{t(($) => $.customers.add_customer)}</DialogTitle>
            <DialogDescription>{t(($) => $.customers.basic_profile)}</DialogDescription>
          </DialogHeader>
          <AccountForm form={form} setForm={setForm} t={t} locale={locale} suggestedTags={suggestedTags} />
          {createAccount.isError && <p className="text-xs text-destructive">{t(($) => $.customers.create_error)}</p>}
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>{t(($) => $.actions.cancel)}</Button>
            <Button disabled={!form.name.trim() || createAccount.isPending} onClick={() => createAccount.mutate()}>{t(($) => $.customers.add_customer)}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
