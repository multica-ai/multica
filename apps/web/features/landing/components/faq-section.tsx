"use client";

import { useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useLocale } from "../i18n";

export function FAQSection() {
  const { t } = useLocale();
  const [openIndex, setOpenIndex] = useState<number | null>(null);

  return (
    <section id="faq" className="bg-[#f8f8f8] text-[#0a0d12]">
      <div className="mx-auto max-w-[860px] px-4 py-24 sm:px-6 sm:py-32 lg:py-40">
        <div className="text-center">
          <p className="text-[11px] font-semibold uppercase tracking-[0.16em] text-[#0a0d12]/40">
            {t.faq.label}
          </p>
          <h2 className="mt-4 font-[family-name:var(--font-serif)] text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem] lg:text-[4.2rem]">
            {t.faq.headline}
          </h2>
        </div>

        <div className="mt-14 divide-y divide-[#0a0d12]/10 sm:mt-16">
          {t.faq.items.map((faq, i) => (
            <div key={i}>
              <button
                onClick={() => setOpenIndex(openIndex === i ? null : i)}
                className="flex w-full items-start justify-between gap-4 py-6 text-left"
              >
                <span className="text-[16px] font-semibold leading-snug text-[#0a0d12] sm:text-[17px]">
                  {faq.question}
                </span>
                <span
                  className={cn(
                    "mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full border border-[#0a0d12]/12 text-[#0a0d12]/40 transition-transform",
                    openIndex === i && "rotate-45",
                  )}
                >
                  <svg
                    width="12"
                    height="12"
                    viewBox="0 0 12 12"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="1.5"
                    strokeLinecap="round"
                  >
                    <path d="M6 1v10M1 6h10" />
                  </svg>
                </span>
              </button>
              <div
                className={cn(
                  "grid transition-[grid-template-rows] duration-200 ease-out",
                  openIndex === i ? "grid-rows-[1fr]" : "grid-rows-[0fr]",
                )}
              >
                <div className="overflow-hidden">
                  <p className="pb-6 pr-12 text-[14px] leading-[1.7] text-[#0a0d12]/56 sm:text-[15px]">
                    {faq.answer}
                  </p>
                </div>
              </div>
            </div>
          ))}
        </div>
      </div>
    </section>
  );
}
