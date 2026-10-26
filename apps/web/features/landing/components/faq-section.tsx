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
      <Container className="flex flex-col items-center">
        <SectionHeader eyebrow={t.faq.label} title={t.faq.headline} className="text-center" />
        <div className="mt-site-block flex w-full max-w-[48rem] flex-col gap-2">
          {t.faq.items.map((faq) => (
            <details
              key={faq.question}
              className="group rounded-site-card border border-site-line bg-site-surface transition-colors open:border-site-line-strong"
            >
              <summary
                className={cn(
                  "flex cursor-pointer list-none items-center justify-between gap-6 rounded-site-card px-5 py-4 text-site-body text-site-ink [&::-webkit-details-marker]:hidden",
                  focusRing,
                )}
              >
                {faq.question}
                <span className="flex size-7 shrink-0 items-center justify-center rounded-full border border-site-line text-site-muted transition-colors group-hover:text-site-ink">
                  <Plus aria-hidden className="size-3.5 transition-transform group-open:rotate-45" />
                </span>
              </summary>
              <p className="-mt-1 max-w-[40rem] px-5 pb-5 text-site-body text-site-muted">
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
