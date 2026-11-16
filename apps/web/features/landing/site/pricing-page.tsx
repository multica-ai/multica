import { Check } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { ActionLink, Container, Section } from "./primitives";
import { CtaBand, PageHero, SitePage } from "./site-page";
import { PLACEHOLDER_PAGES } from "./sitemap";

const PLANS = [
  { name: "Free", price: "$0", cadence: "Placeholder", cta: { label: "Get started", href: "/login" } },
  { name: "Team", price: "$—", cadence: "per seat / month", cta: { label: "Start trial", href: "/login" }, featured: true },
  { name: "Business", price: "$—", cadence: "per seat / month", cta: { label: "Start trial", href: "/login" } },
  { name: "Enterprise", price: "Custom", cadence: "Annual contract", cta: { label: "Contact sales", href: "/contact-sales" } },
];

export function PricingPage() {
  const page = PLACEHOLDER_PAGES.pricing;

  return (
    <SitePage>
      <PageHero eyebrow="Pricing" title={page.title} lead="Placeholder: plans and pricing are still being decided." />
      <Container className="pb-site-section">
        <div className="grid gap-site-grid-col md:grid-cols-2 xl:grid-cols-4">
          {PLANS.map((plan) => (
            <div
              key={plan.name}
              className={cn(
                "flex flex-col rounded-site-card border bg-site-surface p-6",
                plan.featured ? "border-site-ink" : "border-site-line",
              )}
            >
              <h2 className="text-site-card text-site-ink">{plan.name}</h2>
              <p className="mt-6 font-site-display text-site-title text-site-ink tabular-nums">{plan.price}</p>
              <p className="mt-1 text-site-small text-site-muted">{plan.cadence}</p>
              <ActionLink
                href={plan.cta.href}
                tone={plan.featured ? "primary" : "secondary"}
                className="mt-6"
              >
                {plan.cta.label}
              </ActionLink>
              <ul className="mt-8 flex flex-col gap-2.5 border-t border-site-line pt-6">
                {[0, 1, 2, 3].map((index) => (
                  <li key={index} className="flex items-center gap-2.5 text-site-body text-site-ink-secondary">
                    <Check className="size-4 text-site-muted" aria-hidden />
                    Feature placeholder
                  </li>
                ))}
              </ul>
            </div>
          ))}
        </div>
      </Container>
      <Section>
        <Container>
          <div className="rounded-site-card border border-dashed border-site-line-strong p-10 text-site-body text-site-muted">
            Placeholder: plan comparison table.
          </div>
        </Container>
      </Section>
      <CtaBand />
    </SitePage>
  );
}
