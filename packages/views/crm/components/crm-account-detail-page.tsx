"use client";

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Building2, Pencil, Plus, Trash2 } from "lucide-react";
import { api } from "@multica/core/api";
import {
  crmAccountDetailOptions,
  crmCommunicationNoteListOptions,
  crmContactListOptions,
  crmEmailThreadListOptions,
  crmKeys,
} from "@multica/core/crm/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectKeys, projectListOptions } from "@multica/core/projects";
import type {
  CRMAccount,
  CRMAccountPriority,
  CRMAccountRating,
  CRMAccountSource,
  CRMAccountStatus,
  CRMAccountType,
  CRMContact,
  CRMContactDecisionRole,
  CreateCRMContactRequest,
  Project,
} from "@multica/core/types";
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
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { useT } from "../../i18n";
import type crmResources from "../../locales/en/crm.json";
import { PageHeader } from "../../layout/page-header";
import { COUNTRY_OPTIONS, countryByCode, loadCityOptions, loadRegionOptions, localizedName, normalizeLocale, useLocationSelection } from "../geo";

type CRMResources = typeof crmResources;
type Translation = (
  selector: (resources: CRMResources) => string,
  options?: Record<string, unknown>,
) => string;

type Locale = "en" | "zh-Hans";

type AccountFormState = {
  name: string;
  accountCode: string;
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

type ContactFormState = {
  id?: string;
  name: string;
  email: string;
  phone: string;
  mobile: string;
  whatsapp: string;
  wechat: string;
  jobTitle: string;
  department: string;
  decisionRole: CRMContactDecisionRole | "";
  preferredLanguage: string;
  timezone: string;
  isPrimary: boolean;
  notes: string;
};

const toDateTimeLocal = (value?: string | null) => value ? value.slice(0, 16) : "";
const fromDateTimeLocal = (value: string) => value ? new Date(value).toISOString() : null;

function countryName(codeOrName: string | null | undefined, locale: Locale) {
  const country = countryByCode(codeOrName);
  return country ? localizedName(country.name, locale) : codeOrName || "";
}

function accountToForm(account: CRMAccount): AccountFormState {
  const country = countryByCode(account.country_code || account.country);

  return {
    name: account.name,
    accountCode: account.account_code ?? "",
    accountType: account.account_type,
    status: account.status,
    source: account.source ?? "manual",
    rating: account.rating,
    priority: account.priority,
    website: account.website ?? "",
    countryCode: country?.code ?? account.country_code ?? account.country ?? "",
    regionCode: account.region ?? "",
    cityCode: account.city ?? "",
    industry: account.industry ?? "",
    subIndustry: account.sub_industry ?? "",
    annualRevenue: account.annual_revenue ?? "",
    employeeCount: account.employee_count ?? "",
    tags: account.tags?.join(", ") ?? "",
    nextFollowUpAt: toDateTimeLocal(account.next_follow_up_at),
    notes: account.notes ?? "",
  };
}

const blankContactForm = (): ContactFormState => ({
  name: "",
  email: "",
  phone: "",
  mobile: "",
  whatsapp: "",
  wechat: "",
  jobTitle: "",
  department: "",
  decisionRole: "",
  preferredLanguage: "",
  timezone: "",
  isPrimary: false,
  notes: "",
});

function contactToForm(contact: CRMContact): ContactFormState {
  return {
    id: contact.id,
    name: contact.name,
    email: contact.email ?? "",
    phone: contact.phone ?? "",
    mobile: contact.mobile ?? "",
    whatsapp: contact.whatsapp ?? contact.whatsapp_id ?? "",
    wechat: contact.wechat ?? "",
    jobTitle: contact.job_title ?? contact.role_title ?? "",
    department: contact.department ?? "",
    decisionRole: contact.decision_role ?? "",
    preferredLanguage: contact.preferred_language ?? contact.language ?? "",
    timezone: contact.timezone ?? "",
    isPrimary: contact.is_primary,
    notes: contact.notes ?? "",
  };
}

function AccountStatusLabel({ status, t }: { status: CRMAccountStatus; t: Translation }) {
  const labels: Record<CRMAccountStatus, string> = {
    active: t(($) => $.statuses.active),
    inactive: t(($) => $.statuses.inactive),
    prospect: t(($) => $.statuses.prospect),
    archived: t(($) => $.statuses.archived),
  };
  return labels[status] ?? status;
}

function ChannelLabel({ channel, t }: { channel: string; t: Translation }) {
  const labels: Record<string, string> = {
    manual: t(($) => $.channels.manual),
    email: t(($) => $.channels.email),
    whatsapp: t(($) => $.channels.whatsapp),
    phone: t(($) => $.channels.phone),
    meeting: t(($) => $.channels.meeting),
    other: t(($) => $.channels.other),
  };
  return labels[channel] ?? channel;
}

function FieldRow({ label, value }: { label: string; value?: string | null }) {
  return (
    <div className="rounded-md border bg-background/50 p-3">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="mt-1 text-sm">{value || "—"}</div>
    </div>
  );
}

function AccountForm({ form, setForm, t, locale }: { form: AccountFormState; setForm: (next: AccountFormState) => void; t: Translation; locale: Locale }) {
  const { regions, cities, regionsLoading, citiesLoading } = useLocationSelection(form.countryCode, form.regionCode);
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
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.countryCode} onChange={(e) => setForm({ ...form, countryCode: e.target.value, regionCode: "", cityCode: "" })}>
        <option value="">{t(($) => $.customers.country)}</option>
        {COUNTRY_OPTIONS.map((option) => <option key={option.code} value={option.code}>{localizedName(option.name, locale)}</option>)}
      </select>
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.regionCode} onChange={(e) => setForm({ ...form, regionCode: e.target.value, cityCode: "" })} disabled={!form.countryCode || regionsLoading}>
        <option value="">{regionsLoading ? `${t(($) => $.customers.region)}...` : t(($) => $.customers.region)}</option>
        {regions.map((option) => <option key={option.code} value={option.code}>{localizedName(option.name, locale)}</option>)}
      </select>
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.cityCode} onChange={(e) => setForm({ ...form, cityCode: e.target.value })} disabled={!form.regionCode || citiesLoading}>
        <option value="">{citiesLoading ? `${t(($) => $.customers.city)}...` : t(($) => $.customers.city)}</option>
        {cities.map((option) => <option key={option.code} value={option.code}>{localizedName(option.name, locale)}</option>)}
      </select>
      <Input value={form.industry} onChange={(e) => setForm({ ...form, industry: e.target.value })} placeholder={t(($) => $.customers.industry)} />
      <Input value={form.subIndustry} onChange={(e) => setForm({ ...form, subIndustry: e.target.value })} placeholder={t(($) => $.customers.sub_industry)} />
      <Input value={form.annualRevenue} onChange={(e) => setForm({ ...form, annualRevenue: e.target.value })} placeholder={t(($) => $.customers.annual_revenue)} />
      <Input value={form.employeeCount} onChange={(e) => setForm({ ...form, employeeCount: e.target.value })} placeholder={t(($) => $.customers.employee_count)} />
      <Input className="sm:col-span-2" value={form.tags} onChange={(e) => setForm({ ...form, tags: e.target.value })} placeholder={t(($) => $.customers.tags_placeholder)} />
      <Input className="sm:col-span-2" type="datetime-local" value={form.nextFollowUpAt} onChange={(e) => setForm({ ...form, nextFollowUpAt: e.target.value })} />
      <textarea className="min-h-24 rounded-md border bg-background px-3 py-2 text-sm sm:col-span-2" value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} placeholder={t(($) => $.customers.notes)} />
    </div>
  );
}

function ContactForm({ form, setForm, t }: { form: ContactFormState; setForm: (next: ContactFormState) => void; t: Translation }) {
  return (
    <div className="grid max-h-[70vh] gap-3 overflow-y-auto pr-1 sm:grid-cols-2">
      <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder={t(($) => $.contacts.name)} />
      <Input value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} placeholder={t(($) => $.contacts.email)} />
      <Input value={form.phone} onChange={(e) => setForm({ ...form, phone: e.target.value })} placeholder={t(($) => $.contacts.phone)} />
      <Input value={form.mobile} onChange={(e) => setForm({ ...form, mobile: e.target.value })} placeholder={t(($) => $.contacts.mobile)} />
      <Input value={form.whatsapp} onChange={(e) => setForm({ ...form, whatsapp: e.target.value })} placeholder={t(($) => $.contacts.whatsapp)} />
      <Input value={form.wechat} onChange={(e) => setForm({ ...form, wechat: e.target.value })} placeholder={t(($) => $.contacts.wechat)} />
      <Input value={form.jobTitle} onChange={(e) => setForm({ ...form, jobTitle: e.target.value })} placeholder={t(($) => $.contacts.job_title)} />
      <Input value={form.department} onChange={(e) => setForm({ ...form, department: e.target.value })} placeholder={t(($) => $.contacts.department)} />
      <select className="h-9 rounded-md border bg-background px-3 text-sm" value={form.decisionRole} onChange={(e) => setForm({ ...form, decisionRole: e.target.value as CRMContactDecisionRole | "" })}>
        <option value="">{t(($) => $.contacts.decision_role)}</option>
        <option value="decision_maker">{t(($) => $.decision_roles.decision_maker)}</option>
        <option value="influencer">{t(($) => $.decision_roles.influencer)}</option>
        <option value="buyer">{t(($) => $.decision_roles.buyer)}</option>
        <option value="user">{t(($) => $.decision_roles.user)}</option>
        <option value="technical">{t(($) => $.decision_roles.technical)}</option>
        <option value="finance">{t(($) => $.decision_roles.finance)}</option>
        <option value="gatekeeper">{t(($) => $.decision_roles.gatekeeper)}</option>
        <option value="other">{t(($) => $.decision_roles.other)}</option>
      </select>
      <Input value={form.preferredLanguage} onChange={(e) => setForm({ ...form, preferredLanguage: e.target.value })} placeholder={t(($) => $.contacts.preferred_language)} />
      <Input value={form.timezone} onChange={(e) => setForm({ ...form, timezone: e.target.value })} placeholder={t(($) => $.contacts.timezone)} />
      <label className="flex items-center gap-2 text-xs text-muted-foreground">
        <input type="checkbox" checked={form.isPrimary} onChange={(e) => setForm({ ...form, isPrimary: e.target.checked })} />
        {t(($) => $.contacts.is_primary)}
      </label>
      <textarea className="min-h-24 rounded-md border bg-background px-3 py-2 text-sm sm:col-span-2" value={form.notes} onChange={(e) => setForm({ ...form, notes: e.target.value })} placeholder={t(($) => $.contacts.notes)} />
    </div>
  );
}

function contactPayload(form: ContactFormState): CreateCRMContactRequest {
  return {
    name: form.name,
    email: form.email || null,
    phone: form.phone || null,
    mobile: form.mobile || null,
    whatsapp_id: form.whatsapp || null,
    whatsapp: form.whatsapp || null,
    wechat: form.wechat || null,
    job_title: form.jobTitle || null,
    role_title: form.jobTitle || null,
    department: form.department || null,
    decision_role: form.decisionRole || null,
    preferred_language: form.preferredLanguage || null,
    language: form.preferredLanguage || null,
    timezone: form.timezone || null,
    is_primary: form.isPrimary,
    notes: form.notes || null,
  };
}

function projectLinkedToAccount(project: Project, accountId: string) {
  const resources = project.resources ?? [];
  return resources.some((resource) => {
    const ref = resource.resource_ref as { account_id?: string };
    return resource.resource_type === "crm_account" && ref.account_id === accountId;
  });
}

export function CRMAccountDetailPage({ accountId }: { accountId: string }) {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const { t: rawT, i18n } = useT("crm");
  const t = rawT as Translation;
  const locale = normalizeLocale(i18n.language);

  const { data: account, isLoading: accountLoading } = useQuery(crmAccountDetailOptions(wsId, accountId));
  const { data: contacts = [], isLoading: contactsLoading } = useQuery(crmContactListOptions(wsId, accountId));
  const { data: notes = [], isLoading: notesLoading } = useQuery(crmCommunicationNoteListOptions(wsId, accountId));
  const { data: emailThreads = [], isLoading: emailThreadsLoading } = useQuery(crmEmailThreadListOptions(wsId, accountId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));

  const [accountForm, setAccountForm] = useState<AccountFormState | null>(null);
  const [contactForm, setContactForm] = useState<ContactFormState | null>(null);
  const [noteBody, setNoteBody] = useState("");
  const [selectedProjectIds, setSelectedProjectIds] = useState<string[]>([]);
  const [selectedFollowUpProjectId, setSelectedFollowUpProjectId] = useState("");
  const [followUpTitle, setFollowUpTitle] = useState("");

  const linkedProjectIds = useMemo(
    () => projects.filter((project) => projectLinkedToAccount(project, accountId)).map((project) => project.id),
    [accountId, projects],
  );

  useEffect(() => {
    setSelectedProjectIds(linkedProjectIds);
  }, [linkedProjectIds]);

  const updateAccount = useMutation({
    mutationFn: async () => {
      if (!accountForm) throw new Error("missing account form");
      const country = countryByCode(accountForm.countryCode);
      const regions = await loadRegionOptions(accountForm.countryCode);
      const region = regions.find((option) => option.code === accountForm.regionCode);
      const cities = await loadCityOptions(accountForm.countryCode, accountForm.regionCode);
      const city = cities.find((option) => option.code === accountForm.cityCode);
      return api.updateCRMAccount(accountId, {
        name: accountForm.name,
        account_code: accountForm.accountCode || null,
        account_type: accountForm.accountType,
        website: accountForm.website || null,
        country: accountForm.countryCode || null,
        country_code: accountForm.countryCode || null,
        country_name: country ? localizedName(country.name, locale) : null,
        region: region ? localizedName(region.name, locale) : accountForm.regionCode || null,
        city: city ? localizedName(city.name, locale) : accountForm.cityCode || null,
        industry: accountForm.industry || null,
        sub_industry: accountForm.subIndustry || null,
        status: accountForm.status,
        source: accountForm.source,
        rating: accountForm.rating,
        priority: accountForm.priority,
        annual_revenue: accountForm.annualRevenue || null,
        employee_count: accountForm.employeeCount || null,
        tags: accountForm.tags.split(",").map((tag) => tag.trim()).filter(Boolean),
        next_follow_up_at: fromDateTimeLocal(accountForm.nextFollowUpAt),
        notes: accountForm.notes || null,
      });
    },
    onSuccess: async () => {
      setAccountForm(null);
      await queryClient.invalidateQueries({ queryKey: crmKeys.accounts(wsId) });
      await queryClient.invalidateQueries({ queryKey: crmKeys.accountDetail(wsId, accountId) });
    },
  });

  const deleteAccount = useMutation({
    mutationFn: () => api.deleteCRMAccount(accountId),
    onSuccess: () => window.close(),
  });

  const saveContact = useMutation({
    mutationFn: () => {
      if (!contactForm) throw new Error("missing contact form");
      return contactForm.id
        ? api.updateCRMContact(accountId, contactForm.id, contactPayload(contactForm))
        : api.createCRMContact(accountId, contactPayload(contactForm));
    },
    onSuccess: async () => {
      setContactForm(null);
      await queryClient.invalidateQueries({ queryKey: crmKeys.accounts(wsId) });
      await queryClient.invalidateQueries({ queryKey: crmKeys.contacts(wsId, accountId) });
    },
  });

  const deleteContact = useMutation({
    mutationFn: (contactId: string) => api.deleteCRMContact(accountId, contactId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: crmKeys.accounts(wsId) });
      await queryClient.invalidateQueries({ queryKey: crmKeys.contacts(wsId, accountId) });
    },
  });

  const createNote = useMutation({
    mutationFn: () => api.createCRMCommunicationNote(accountId, { body: noteBody, channel: "manual", direction: "note" }),
    onSuccess: async () => {
      setNoteBody("");
      await queryClient.invalidateQueries({ queryKey: crmKeys.notes(wsId, accountId) });
    },
  });

  const linkProject = useMutation({
    mutationFn: () => api.linkCRMAccountProject(accountId, { project_ids: selectedProjectIds }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: projectKeys.list(wsId) });
    },
  });

  const createProject = useMutation({
    mutationFn: async () => {
      if (!account) throw new Error("missing account");
      return api.createProject({
        title: `CRM:${account.name}`,
        description: account.notes ?? undefined,
        status: "planned",
        priority: "medium",
        resources: [{
          resource_type: "crm_account",
          resource_ref: { account_id: account.id, name: account.name },
          label: account.name,
        }],
      });
    },
    onSuccess: async (project) => {
      await queryClient.invalidateQueries({ queryKey: projectKeys.list(wsId) });
      setSelectedProjectIds((current) => Array.from(new Set([...current, project.id])));
    },
  });

  const createFollowUp = useMutation({
    mutationFn: () => api.createCRMFollowUpIssue(accountId, {
      project_id: selectedFollowUpProjectId || null,
      title: followUpTitle || t(($) => $.projects.follow_up_placeholder, { name: account?.name ?? "" }),
      priority: "medium",
    }),
    onSuccess: async () => {
      setFollowUpTitle("");
      await queryClient.invalidateQueries({ queryKey: crmKeys.accountDetail(wsId, accountId) });
    },
  });

  if (accountLoading || !account) {
    return (
      <div className="flex h-full items-center justify-center">
        <Skeleton className="h-16 w-80" />
      </div>
    );
  }

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <Building2 className="size-4 text-muted-foreground" />
            <h1 className="truncate text-sm font-medium">{account.name}</h1>
            <span className="rounded-full bg-emerald-500/10 px-2 py-0.5 text-xs text-emerald-700 dark:text-emerald-300">
              <AccountStatusLabel status={account.status} t={t} />
            </span>
          </div>
          <p className="mt-1 text-xs text-muted-foreground">
            {[account.website, countryName(account.country_code || account.country_name || account.country, locale), account.region, account.industry].filter(Boolean).join(" · ") || t(($) => $.customers.basic_profile)}
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => setAccountForm(accountToForm(account))}>
            <Pencil className="mr-1 size-4" /> {t(($) => $.actions.edit)}
          </Button>
          <Button size="sm" variant="outline" disabled={deleteAccount.isPending} onClick={() => window.confirm(t(($) => $.customers.delete_confirm)) && deleteAccount.mutate()}>
            <Trash2 className="mr-1 size-4" /> {t(($) => $.actions.delete)}
          </Button>
        </div>
      </PageHeader>

      <Tabs defaultValue="overview" className="min-h-0 flex-1 gap-0">
        <div className="border-b px-6 py-3">
          <TabsList variant="line" className="w-full justify-start overflow-x-auto">
            <TabsTrigger value="overview">{t(($) => $.tabs.overview)}</TabsTrigger>
            <TabsTrigger value="contacts">{t(($) => $.tabs.contacts)}</TabsTrigger>
            <TabsTrigger value="profile">{t(($) => $.tabs.profile)}</TabsTrigger>
            <TabsTrigger value="projects">{t(($) => $.tabs.projects)}</TabsTrigger>
            <TabsTrigger value="emails">{t(($) => $.tabs.emails)}</TabsTrigger>
            <TabsTrigger value="notes">{t(($) => $.tabs.notes)}</TabsTrigger>
          </TabsList>
        </div>

        <div className="min-h-0 flex-1 overflow-y-auto p-6">
          <TabsContent value="overview" className="space-y-6">
            <section className="rounded-lg border bg-card p-4">
              <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
                <FieldRow label={t(($) => $.customers.status)} value={t(($) => $.statuses[account.status])} />
                <FieldRow label={t(($) => $.customers.account_type)} value={t(($) => $.account_types[account.account_type])} />
                <FieldRow label={t(($) => $.customers.rating)} value={t(($) => $.ratings[account.rating])} />
                <FieldRow label={t(($) => $.customers.priority)} value={t(($) => $.priorities[account.priority])} />
                <FieldRow label={t(($) => $.customers.website)} value={account.website} />
                <FieldRow label={t(($) => $.customers.country)} value={countryName(account.country_code || account.country_name || account.country, locale)} />
                <FieldRow label={t(($) => $.customers.region)} value={account.region} />
                <FieldRow label={t(($) => $.customers.city)} value={account.city} />
                <FieldRow label={t(($) => $.customers.industry)} value={[account.industry, account.sub_industry].filter(Boolean).join(" · ")} />
                <FieldRow label={t(($) => $.customers.last_contacted_at)} value={account.last_contacted_at ? new Date(account.last_contacted_at).toLocaleString() : null} />
                <FieldRow label={t(($) => $.customers.next_follow_up_at)} value={account.next_follow_up_at ? new Date(account.next_follow_up_at).toLocaleString() : null} />
              </div>
              {account.notes && <p className="mt-4 whitespace-pre-wrap rounded-md border bg-background/50 p-3 text-sm">{account.notes}</p>}
            </section>
          </TabsContent>

          <TabsContent value="contacts" className="space-y-4">
            <div className="flex justify-end">
              <Button size="sm" onClick={() => setContactForm(blankContactForm())}>
                <Plus className="mr-1 size-4" /> {t(($) => $.contacts.add)}
              </Button>
            </div>
            <section className="rounded-lg border bg-card">
              {contactsLoading ? (
                <div className="space-y-2 p-4"><Skeleton className="h-12 w-full" /><Skeleton className="h-12 w-full" /></div>
              ) : contacts.length === 0 ? (
                <div className="p-10 text-center text-sm text-muted-foreground">{t(($) => $.contacts.empty)}</div>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t(($) => $.contacts.name)}</TableHead>
                      <TableHead>{t(($) => $.contacts.job_title)}</TableHead>
                      <TableHead>{t(($) => $.contacts.email)}</TableHead>
                      <TableHead>{t(($) => $.contacts.whatsapp)}</TableHead>
                      <TableHead>{t(($) => $.contacts.is_primary)}</TableHead>
                      <TableHead></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {contacts.map((contact) => (
                      <TableRow key={contact.id}>
                        <TableCell className="font-medium">{contact.name}</TableCell>
                        <TableCell>{contact.job_title || contact.role_title || "—"}</TableCell>
                        <TableCell>{contact.email || "—"}</TableCell>
                        <TableCell>{contact.whatsapp || contact.whatsapp_id || "—"}</TableCell>
                        <TableCell>{contact.is_primary ? "✓" : "—"}</TableCell>
                        <TableCell className="text-right">
                          <Button size="sm" variant="ghost" onClick={() => setContactForm(contactToForm(contact))}><Pencil className="size-4" /></Button>
                          <Button size="sm" variant="ghost" disabled={deleteContact.isPending} onClick={() => window.confirm("Delete this contact?") && deleteContact.mutate(contact.id)}><Trash2 className="size-4" /></Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </section>
          </TabsContent>

          <TabsContent value="profile" className="space-y-6">
            <section className="rounded-lg border bg-card p-4">
              <FieldRow label={t(($) => $.profile.summary_title)} value={account.notes} />
              <div className="mt-3 grid gap-3 sm:grid-cols-2">
                <FieldRow label={t(($) => $.profile.metadata_title)} value={[countryName(account.country_code || account.country_name || account.country, locale), account.region, account.city, account.industry].filter(Boolean).join(" · ")} />
                <FieldRow label={t(($) => $.customers.source)} value={account.source ? t(($) => $.sources[account.source!]) : null} />
                <FieldRow label={t(($) => $.customers.tags)} value={account.tags?.join(", ")} />
              </div>
            </section>
          </TabsContent>

          <TabsContent value="projects" className="grid gap-6 lg:grid-cols-2">
            <section className="rounded-lg border bg-card p-4">
              <h3 className="text-sm font-medium">{t(($) => $.projects.link_title)}</h3>
              <div className="mt-4 space-y-3">
                <select multiple className="min-h-40 w-full rounded-md border bg-background px-3 py-2 text-sm" value={selectedProjectIds} onChange={(e) => setSelectedProjectIds(Array.from(e.target.selectedOptions, (option) => option.value))}>
                  {projects.map((project) => <option key={project.id} value={project.id}>{project.title}</option>)}
                </select>
                <Button className="w-full" variant="outline" disabled={linkProject.isPending} onClick={() => linkProject.mutate()}>{t(($) => $.projects.link)}</Button>
                <Button className="w-full" disabled={createProject.isPending} onClick={() => createProject.mutate()}><Plus className="mr-1 size-4" /> {t(($) => $.projects.create_linked_project)}</Button>
                {linkProject.isError && <p className="text-xs text-destructive">{t(($) => $.projects.link_error)}</p>}
                {linkProject.isSuccess && <p className="text-xs text-emerald-600 dark:text-emerald-400">{t(($) => $.projects.link_success)}</p>}
                {createProject.isError && <p className="text-xs text-destructive">{t(($) => $.projects.create_project_error)}</p>}
                {createProject.isSuccess && <p className="text-xs text-emerald-600 dark:text-emerald-400">{t(($) => $.projects.create_project_success)}</p>}
              </div>
            </section>
            <section className="rounded-lg border bg-card p-4">
              <h3 className="text-sm font-medium">{t(($) => $.projects.follow_up_title)}</h3>
              <div className="mt-4 space-y-3">
                <select className="h-9 w-full rounded-md border bg-background px-3 text-sm" value={selectedFollowUpProjectId} onChange={(e) => setSelectedFollowUpProjectId(e.target.value)}>
                  <option value="">{t(($) => $.projects.select)}</option>
                  {projects.map((project) => <option key={project.id} value={project.id}>{project.title}</option>)}
                </select>
                <Input value={followUpTitle} onChange={(e) => setFollowUpTitle(e.target.value)} placeholder={t(($) => $.projects.follow_up_placeholder, { name: account.name })} />
                <Button className="w-full" disabled={createFollowUp.isPending} onClick={() => createFollowUp.mutate()}>{t(($) => $.projects.create_follow_up)}</Button>
              </div>
            </section>
          </TabsContent>

          <TabsContent value="emails" className="space-y-6">
            <section className="rounded-lg border bg-card">
              {emailThreadsLoading ? <div className="space-y-2 p-4"><Skeleton className="h-16 w-full" /><Skeleton className="h-16 w-full" /></div> : emailThreads.length === 0 ? <div className="p-10 text-center text-sm text-muted-foreground">{t(($) => $.emails.account_empty)}</div> : <div className="divide-y">{emailThreads.map((thread) => <div key={thread.id} className="px-4 py-3 text-sm"><div className="font-medium">{thread.subject}</div><div className="mt-1 text-xs text-muted-foreground">{[thread.mailbox, thread.direction, thread.status, t(($) => $.common.count_messages, { count: thread.message_count })].filter(Boolean).join(" · ")}</div></div>)}</div>}
            </section>
          </TabsContent>

          <TabsContent value="notes" className="space-y-6">
            <section className="rounded-lg border bg-card">
              <div className="border-b p-4">
                <textarea className="min-h-24 w-full rounded-md border bg-background px-3 py-2 text-sm" value={noteBody} onChange={(e) => setNoteBody(e.target.value)} placeholder={t(($) => $.notes.placeholder)} />
                <div className="mt-2 flex justify-end"><Button size="sm" disabled={!noteBody.trim() || createNote.isPending} onClick={() => createNote.mutate()}><Plus className="mr-1 size-4" /> {t(($) => $.notes.add)}</Button></div>
              </div>
              {notesLoading ? <div className="space-y-2 p-4"><Skeleton className="h-16 w-full" /><Skeleton className="h-16 w-full" /></div> : notes.length === 0 ? <div className="p-8 text-center text-sm text-muted-foreground">{t(($) => $.notes.empty)}</div> : <div className="divide-y">{notes.map((note) => <div key={note.id} className="px-4 py-3 text-sm"><div className="whitespace-pre-wrap">{note.body}</div><div className="mt-2 text-xs text-muted-foreground"><ChannelLabel channel={note.channel} t={t} /> · {new Date(note.occurred_at).toLocaleString()}</div></div>)}</div>}
            </section>
          </TabsContent>
        </div>
      </Tabs>

      <Dialog open={accountForm !== null} onOpenChange={(open) => !open && setAccountForm(null)}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader><DialogTitle>{t(($) => $.customers.edit_customer)}</DialogTitle><DialogDescription>{account.name}</DialogDescription></DialogHeader>
          {accountForm && <AccountForm form={accountForm} setForm={setAccountForm} t={t} locale={locale} />}
          {updateAccount.isError && <p className="text-xs text-destructive">{t(($) => $.customers.create_error)}</p>}
          <DialogFooter><Button variant="outline" onClick={() => setAccountForm(null)}>{t(($) => $.actions.cancel)}</Button><Button disabled={!accountForm?.name.trim() || updateAccount.isPending} onClick={() => updateAccount.mutate()}>{t(($) => $.actions.save)}</Button></DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={contactForm !== null} onOpenChange={(open) => !open && setContactForm(null)}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader><DialogTitle>{contactForm?.id ? t(($) => $.contacts.edit_title) : t(($) => $.contacts.add_title)}</DialogTitle><DialogDescription>{account.name}</DialogDescription></DialogHeader>
          {contactForm && <ContactForm form={contactForm} setForm={setContactForm} t={t} />}
          {saveContact.isError && <p className="text-xs text-destructive">{t(($) => $.contacts.create_error)}</p>}
          <DialogFooter><Button variant="outline" onClick={() => setContactForm(null)}>{t(($) => $.actions.cancel)}</Button><Button disabled={!contactForm?.name.trim() || saveContact.isPending} onClick={() => saveContact.mutate()}>{t(($) => $.actions.save)}</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
