"use client";

import { Plus } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Container, Section, SectionHeader, focusRing } from "../site/primitives";
import { InlineLinks } from "./inline-links";
import { useLocale } from "../i18n";

export function FAQSection() {
  const { t } = useLocale();

  return (
    <Section id="faq">
      <Container className="grid gap-10 lg:grid-cols-[minmax(0,1fr)_minmax(0,2fr)] lg:gap-16">
        <SectionHeader eyebrow={t.faq.label} title={t.faq.headline} />
        <div className="divide-y divide-site-line border-y border-site-line">
          {t.faq.items.map((faq) => (
            <details key={faq.question} className="group">
              <summary
                className={cn(
                  "flex cursor-pointer list-none items-start justify-between gap-6 py-5 text-site-card text-site-ink [&::-webkit-details-marker]:hidden",
                  focusRing,
                )}
              >
                {faq.question}
                <Plus
                  aria-hidden
                  className="mt-1 size-4 shrink-0 text-site-muted transition-transform group-open:rotate-45"
                />
              </summary>
              <p className="max-w-[40rem] pb-6 text-site-body text-site-muted">
                <InlineLinks
                  text={faq.answer}
                  linkClassName="text-site-ink underline decoration-site-line-strong underline-offset-4 hover:decoration-site-ink"
                />
              </p>
            </details>
          ))}
        </div>
      </Container>
    </Section>
  );
}
