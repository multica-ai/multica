"use client";

import { useId, useState } from "react";
import { Plus } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Container, Section, SectionHeader, focusRing } from "../site/primitives";
import { InlineLinks } from "./inline-links";
import { useLocale } from "../i18n";

const EASE = "ease-[cubic-bezier(0.32,0.72,0,1)]";

function FAQItem({ question, answer }: { question: string; answer: string }) {
  const [open, setOpen] = useState(false);
  const id = useId();
  const panelId = `${id}-panel`;
  const buttonId = `${id}-button`;

  return (
    <div
      className={cn(
        "rounded-site-card border bg-site-surface transition-colors duration-300",
        open ? "border-site-line-strong" : "border-site-line",
      )}
    >
      <h3>
        <button
          type="button"
          id={buttonId}
          aria-expanded={open}
          aria-controls={panelId}
          onClick={() => setOpen((current) => !current)}
          className={cn(
            "group flex w-full items-center justify-between gap-6 rounded-site-card px-5 py-4 text-left text-site-body text-site-ink",
            focusRing,
          )}
        >
          {question}
          <span className="flex size-7 shrink-0 items-center justify-center rounded-full border border-site-line text-site-muted transition-colors group-hover:text-site-ink">
            <Plus
              aria-hidden
              className={cn("size-3.5 transition-transform duration-300 motion-reduce:transition-none", EASE, open && "rotate-45")}
            />
          </span>
        </button>
      </h3>
      <div
        id={panelId}
        role="region"
        aria-labelledby={buttonId}
        inert={!open}
        className={cn(
          "grid transition-[grid-template-rows,opacity] duration-300 motion-reduce:transition-none",
          EASE,
          open ? "grid-rows-[1fr] opacity-100" : "grid-rows-[0fr] opacity-0",
        )}
      >
        <div className="overflow-hidden">
          <p className="-mt-1 px-5 pb-5 text-site-body text-site-muted">
            <InlineLinks
              text={answer}
              linkClassName="text-site-ink underline decoration-site-line-strong underline-offset-4 hover:decoration-site-ink"
            />
          </p>
        </div>
      </div>
    </div>
  );
}

export function FAQSection() {
  const { t } = useLocale();

  return (
    <Section id="faq">
      <Container className="flex flex-col items-center">
        <SectionHeader eyebrow={t.faq.label} title={t.faq.headline} className="text-center" />
        <div className="mt-site-block flex w-full max-w-[48rem] flex-col gap-2">
          {t.faq.items.map((faq) => (
            <FAQItem key={faq.question} question={faq.question} answer={faq.answer} />
          ))}
        </div>
      </Container>
    </Section>
  );
}
