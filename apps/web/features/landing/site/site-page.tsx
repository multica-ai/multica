import type { ReactNode } from "react";
import Link from "@/features/landing/components/landing-link";
import { cn } from "@multica/ui/lib/utils";
import {
  ActionLink,
  Container,
  Eyebrow,
  ProductFrame,
  Section,
  SectionHeader,
  focusRing,
} from "./primitives";
import { CtaBand } from "./cta-band";
import { SiteFooter } from "./site-footer";
import { SiteHeader } from "./site-header";
import type { PageGroup, SitePage as SitePageEntry } from "./sitemap";

export { CtaBand };

export function SitePage({ children }: { children: ReactNode }) {
  return (
    <>
      <SiteHeader />
      <main className="bg-site-canvas text-site-ink">{children}</main>
      <SiteFooter />
    </>
  );
}

export function PageHero({
  eyebrow,
  title,
  lead,
  actions,
}: {
  eyebrow?: string;
  title: ReactNode;
  lead?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <Container className="pt-site-hero pb-site-section">
      {eyebrow ? <Eyebrow>{eyebrow}</Eyebrow> : null}
      <h1 className="mt-3 max-w-[20ch] font-site-display text-site-heading text-site-ink text-balance">{title}</h1>
      {lead ? (
        <p className="mt-4 max-w-[30rem] text-site-body text-site-muted text-pretty">{lead}</p>
      ) : null}
      {actions ? <div className="mt-7 flex flex-wrap gap-2.5">{actions}</div> : null}
    </Container>
  );
}

/** Landing page for a section (`/product`, `/solutions`) listing its pages by group. */
export function DirectoryPage({
  title,
  lead,
  groups,
  href,
}: {
  title: string;
  lead: string;
  groups: PageGroup[];
  href: (slug: string) => string;
}) {
  return (
    <SitePage>
      <PageHero title={title} lead={lead} />
      <Container className="flex flex-col gap-site-block pb-site-section">
        {groups.map((group) => (
          <section key={group.title} aria-labelledby={`group-${group.title}`}>
            <h2 id={`group-${group.title}`} className="text-site-small text-site-muted">
              {group.title}
            </h2>
            <div className="mt-4 grid gap-site-grid-col sm:grid-cols-2 lg:grid-cols-3">
              {group.pages.map((page) => (
                <Link
                  key={page.slug}
                  href={href(page.slug)}
                  className={cn(
                    "rounded-site-card border border-site-line bg-site-surface p-6 transition-colors hover:border-site-line-strong",
                    focusRing,
                  )}
                >
                  <h3 className="text-site-card text-site-ink">{page.title}</h3>
                  <p className="mt-site-stack text-site-body text-site-muted">{page.summary}</p>
                </Link>
              ))}
            </div>
          </section>
        ))}
      </Container>
      <CtaBand />
    </SitePage>
  );
}

const OUTLINE = [
  { title: "Overview", body: "Placeholder: what this is and who it is for." },
  { title: "How it works", body: "Placeholder: the two or three moments that show it in action." },
  { title: "Proof", body: "Placeholder: a customer quote, a metric, or a short case study." },
];

/** Skeleton shared by every page whose content has not been written yet. */
export function PlaceholderPage({ eyebrow, page }: { eyebrow: string; page: SitePageEntry }) {
  return (
    <SitePage>
      <PageHero
        eyebrow={eyebrow}
        title={page.title}
        lead={page.summary}
        actions={
          <>
            <ActionLink href="/login" size="lg">
              Get started
            </ActionLink>
            <ActionLink href="/contact-sales" size="lg" tone="secondary">
              Book a demo
            </ActionLink>
          </>
        }
      />
      <Container className="pb-site-section">
        <ProductFrame label={`${page.title} visual`} aspectRatio="16 / 8" />
      </Container>
      <Section>
        <Container>
          <SectionHeader title="Sections to come" lead="This page is a placeholder in the new site structure." />
          <div className="mt-site-block grid gap-site-grid-col md:grid-cols-3">
            {OUTLINE.map((item, index) => (
              <div key={item.title} className="rounded-site-card border border-site-line bg-site-surface p-6">
                <p className="font-site-mono text-site-label text-site-muted tabular-nums">
                  {String(index + 1).padStart(2, "0")}
                </p>
                <h3 className="mt-6 text-site-card text-site-ink">{item.title}</h3>
                <p className="mt-site-stack text-site-body text-site-muted">{item.body}</p>
              </div>
            ))}
          </div>
        </Container>
      </Section>
      <CtaBand />
    </SitePage>
  );
}
