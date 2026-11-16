"use client";

import { useState } from "react";
import { Bot, Check, Cloud, Minus, Server } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { useLocale } from "../i18n";
import type { PricingCell, PricingDict, PricingPlanId } from "../i18n/types";
import { ActionLink, Container, Eyebrow, Section, SectionHeader, SiteTextLink } from "./primitives";
import { CtaBand, SitePage } from "./site-page";

type BillingPeriod = "year" | "month";

/** Last day to subscribe yearly with the first-year discount; set from the launch date. */
const LAUNCH_OFFER_ENDS = "2027-02-28";
const FIRST_YEAR_RATE = 0.7;

const PLAN_IDS: PricingPlanId[] = ["free", "pro", "business", "enterprise"];
const FEATURED: PricingPlanId = "business";

/** USD per member per month. */
const SEAT_PRICES: Partial<Record<PricingPlanId, Record<BillingPeriod, number>>> = {
  pro: { year: 12, month: 15 },
  business: { year: 24, month: 30 },
};

const CTA_HREF: Record<PricingPlanId, string> = {
  free: "/login",
  pro: "/login",
  business: "/login",
  enterprise: "/contact-sales",
};

function usd(amount: number) {
  return Number.isInteger(amount) ? `$${amount}` : `$${amount.toFixed(2)}`;
}

function fill(template: string, values: Record<string, string>) {
  return template.replace(/\{(\w+)\}/g, (match, key: string) => values[key] ?? match);
}

function SoonBadge({ label }: { label: string }) {
  return (
    <span className="ml-1.5 inline-flex h-5 items-center rounded-full border border-site-line px-1.5 font-site-mono text-[0.625rem] leading-none tracking-wider text-site-muted uppercase">
      {label}
    </span>
  );
}

function LaunchOffer({ t, date }: { t: PricingDict; date: string }) {
  return (
    <p className="inline-flex items-center gap-2 rounded-full bg-site-inverse py-1 pr-3.5 pl-1 text-site-small text-site-inverse-ink">
      <span className="shrink-0 rounded-full bg-site-canvas px-2 py-0.5 font-site-mono text-[0.6875rem] leading-4 tracking-wider text-site-ink uppercase">
        {t.launchOffer.badge}
      </span>
      {fill(t.launchOffer.text, { date })}
    </p>
  );
}

function BillingToggle({
  t,
  period,
  onChange,
}: {
  t: PricingDict;
  period: BillingPeriod;
  onChange: (period: BillingPeriod) => void;
}) {
  const option = (value: BillingPeriod, label: string) => (
    <button
      type="button"
      role="radio"
      aria-checked={period === value}
      onClick={() => onChange(value)}
      className={cn(
        "rounded-full px-3.5 py-1.5 transition-colors",
        period === value ? "bg-site-inverse text-site-inverse-ink" : "text-site-muted hover:text-site-ink",
      )}
    >
      {label}
    </button>
  );
  return (
    <div
      role="radiogroup"
      aria-label={t.billing.label}
      className="inline-flex items-center gap-1 rounded-full border border-site-line bg-site-surface p-1 text-site-small"
    >
      {option("year", t.billing.yearly)}
      {option("month", t.billing.monthly)}
      <span className="mr-2 ml-1 text-site-small text-site-ink-secondary">{t.billing.save}</span>
    </div>
  );
}

function PlanCard({ id, t, period }: { id: PricingPlanId; t: PricingDict; period: BillingPeriod }) {
  const plan = t.plans[id];
  const featured = id === FEATURED;
  const seat = SEAT_PRICES[id];
  const price = seat ? usd(seat[period]) : id === "free" ? "$0" : t.customPrice;
  const note = seat
    ? period === "year"
      ? fill(t.yearlyNote, { price: usd(seat.month) })
      : fill(t.monthlyNote, { price: usd(seat.year) })
    : "";
  const offer = seat && period === "year" ? fill(t.firstYear, { price: usd(seat.year * FIRST_YEAR_RATE) }) : "";

  return (
    <div
      className={cn(
        "flex flex-col rounded-site-card border bg-site-surface p-6",
        featured ? "border-site-ink shadow-site-float" : "border-site-line",
      )}
    >
      <div className="flex items-center justify-between">
        <h2 className="text-site-card text-site-ink">{plan.name}</h2>
        {featured ? (
          <span className="rounded-full bg-site-inverse px-2 py-0.5 text-[0.6875rem] leading-4 text-site-inverse-ink">
            {t.recommended}
          </span>
        ) : null}
      </div>
      <p className="mt-2 min-h-[2.8em] text-site-small text-site-muted text-pretty">{plan.audience}</p>
      <div className="mt-6 flex items-baseline gap-2">
        <span className="font-site-display text-site-title text-site-ink tabular-nums">{price}</span>
        <span className="text-site-small text-site-muted">{plan.cadence}</span>
      </div>
      <p className="mt-1 h-5 text-site-small text-site-faint">{note}</p>
      <p className="mt-1 h-5 text-site-small text-site-ink">
        {offer ? (
          <>
            <span className="mr-1.5 inline-block size-1.5 -translate-y-px rounded-full bg-site-ink align-middle" />
            {offer}
          </>
        ) : null}
      </p>
      <ActionLink href={CTA_HREF[id]} tone={featured ? "primary" : "secondary"} size="lg" className="mt-5 w-full">
        {plan.cta}
      </ActionLink>
      <p className="mt-2.5 text-center text-site-small text-site-muted">
        {plan.ctaNote}
        {plan.ctaNoteLink ? (
          <>
            {" "}
            <SiteTextLink href="/contact-sales" className="text-site-ink underline underline-offset-2">
              {plan.ctaNoteLink}
            </SiteTextLink>
          </>
        ) : null}
      </p>
      <p className="mt-8 border-t border-site-line pt-6 text-site-small text-site-ink">{plan.includes}</p>
      <ul className="mt-3 flex flex-col gap-2.5">
        {plan.features.map((feature) => (
          <li key={feature.label} className="flex items-start gap-2.5 text-site-small text-site-ink-secondary">
            <Check className="mt-0.5 size-4 shrink-0 text-site-ink" aria-hidden />
            <span>
              {feature.label}
              {feature.soon ? <SoonBadge label={t.soon} /> : null}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

function CellValue({ value, t }: { value: PricingCell; t: PricingDict }) {
  if (value === true) return <Check className="mx-auto size-4 text-site-ink" aria-label={t.included} />;
  if (value === false) return <Minus className="mx-auto size-4 text-site-faint" aria-label={t.notIncluded} />;
  return <span className="text-site-small text-site-ink-secondary">{value}</span>;
}

function ComparisonTable({ t }: { t: PricingDict }) {
  return (
    <div className="mt-site-block overflow-x-auto">
      <table className="w-full min-w-[48rem] border-collapse text-left">
        <thead>
          <tr className="border-b border-site-line">
            <th className="w-[28%] py-4 font-normal" />
            {PLAN_IDS.map((id) => (
              <th key={id} scope="col" className="w-[18%] py-4 text-center text-site-body font-normal text-site-ink">
                {t.plans[id].name}
              </th>
            ))}
          </tr>
        </thead>
        {t.compare.groups.map((group) => (
          <tbody key={group.title}>
            <tr>
              <th
                scope="colgroup"
                colSpan={5}
                className="pt-8 pb-3 font-site-mono text-site-label font-normal text-site-muted uppercase"
              >
                {group.title}
              </th>
            </tr>
            {group.rows.map((row) => (
              <tr key={row.label} className="border-b border-site-rule">
                <th scope="row" className="py-3 text-site-small font-normal text-site-ink">
                  {row.label}
                  {row.soon ? <SoonBadge label={t.soon} /> : null}
                  {row.hint ? <span className="block text-site-small text-site-faint">{row.hint}</span> : null}
                </th>
                {row.cells.map((cell, index) => (
                  <td key={PLAN_IDS[index]} className="py-3 text-center">
                    <CellValue value={cell} t={t} />
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        ))}
      </table>
    </div>
  );
}

export function PricingPage() {
  const { t: dict, locale } = useLocale();
  const t = dict.pricing;
  const [period, setPeriod] = useState<BillingPeriod>("year");
  const offerEnds = new Intl.DateTimeFormat(locale, { dateStyle: "long", timeZone: "UTC" }).format(
    new Date(`${LAUNCH_OFFER_ENDS}T00:00:00Z`),
  );

  return (
    <SitePage>
      {/* Korean wraps between words; Chinese and Japanese must keep breaking between characters. */}
      <div className={cn(locale === "ko" && "break-keep")}>
        <Container className="flex flex-col items-center pt-site-hero pb-12 text-center">
          <Eyebrow>{t.eyebrow}</Eyebrow>
          <h1 className="mt-3 max-w-[22ch] font-site-display text-site-heading text-site-ink text-balance">{t.title}</h1>
          <p className="mt-4 max-w-[36rem] text-site-body text-site-muted text-pretty">{t.lead}</p>
          <div className="mt-8 flex flex-col items-center gap-3">
            <LaunchOffer t={t} date={offerEnds} />
            <BillingToggle t={t} period={period} onChange={setPeriod} />
          </div>
        </Container>

        <Container className="pb-site-section">
          <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
            {PLAN_IDS.map((id) => (
              <PlanCard key={id} id={id} t={t} period={period} />
            ))}
          </div>

          <div className="mt-4 grid gap-4 md:grid-cols-2">
            <div className="flex items-start gap-4 rounded-site-card border border-site-line bg-site-sunken p-6">
              <Cloud className="mt-0.5 size-5 shrink-0 text-site-ink" aria-hidden />
              <div>
                <p className="text-site-body text-site-ink">
                  {t.cloudCredits.title}
                  <SoonBadge label={t.soon} />
                </p>
                <p className="mt-1.5 text-site-small text-site-muted text-pretty">{t.cloudCredits.body}</p>
              </div>
            </div>
            <div className="flex items-start gap-4 rounded-site-card border border-site-line bg-site-sunken p-6">
              <Server className="mt-0.5 size-5 shrink-0 text-site-ink" aria-hidden />
              <div>
                <p className="text-site-body text-site-ink">{t.selfHost.title}</p>
                <p className="mt-1.5 text-site-small text-site-muted text-pretty">{t.selfHost.body}</p>
                <div className="mt-4 flex gap-2">
                  <ActionLink href="/contact-sales" tone="secondary">
                    {t.selfHost.contactSales}
                  </ActionLink>
                  <ActionLink href="/licensing" tone="ghost">
                    {t.selfHost.licensing}
                  </ActionLink>
                </div>
              </div>
            </div>
          </div>
        </Container>

        <Section>
          <Container>
            <SectionHeader title={t.compare.title} lead={t.compare.lead} />
            <ComparisonTable t={t} />
          </Container>
        </Section>

        <Section>
          <Container className="grid gap-site-block lg:grid-cols-[1fr_2fr]">
            <div className="flex items-start gap-3">
              <Bot className="mt-1.5 size-5 text-site-ink" aria-hidden />
              <h2 className="font-site-display text-site-heading text-site-ink">{t.faq.title}</h2>
            </div>
            <dl className="flex flex-col">
              {t.faq.items.map((item) => (
                <div key={item.question} className="border-b border-site-line py-5 first:pt-0">
                  <dt className="text-site-body text-site-ink">{item.question}</dt>
                  <dd className="mt-2 text-site-small text-site-muted text-pretty">
                    {fill(item.answer, { date: offerEnds })}
                  </dd>
                </div>
              ))}
            </dl>
          </Container>
        </Section>
      </div>
      <CtaBand />
    </SitePage>
  );
}
