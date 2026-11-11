"use client";

import { useLocale } from "../i18n";
import { ActionLink, Container, Section } from "./primitives";

export function CtaBand({ title, lead }: { title?: string; lead?: string }) {
  const { t } = useLocale();
  const copy = t.site.cta;
  return (
    <Section className="py-0">
      <Container className="flex flex-col items-center pt-24 pb-16 text-center lg:pt-30 lg:pb-20">
        <h2 className="max-w-[16ch] font-site-display text-site-display text-site-ink text-balance">{title ?? copy.title}</h2>
        <p className="mt-4 max-w-[30rem] text-site-body text-site-muted text-balance">{lead ?? copy.lead}</p>
        <div className="mt-8 flex flex-wrap justify-center gap-2.5">
          <ActionLink href="/login" size="lg">
            {copy.getStarted}
          </ActionLink>
          <ActionLink href="/contact-sales" size="lg" tone="secondary">
            {copy.bookDemo}
          </ActionLink>
        </div>
      </Container>
    </Section>
  );
}
