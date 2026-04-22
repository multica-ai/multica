"use client";

import { useState } from "react";
import { CloudWaitlistExpand } from "@multica/views/onboarding";
import { useLocale } from "../../i18n";

/**
 * Cloud runtime waitlist — thin wrapper around the shared
 * CloudWaitlistExpand form with a download-page-appropriate title
 * and subtitle. Submission persists via `joinCloudWaitlist` inside
 * the child; the submitted flag here only prevents double-submits
 * for the lifetime of the page.
 */
export function CloudSection() {
  const { t } = useLocale();
  const d = t.download.cloud;
  const [submitted, setSubmitted] = useState(false);

  return (
    <section className="bg-white py-20 text-[#0a0d12] sm:py-24">
      <div className="mx-auto max-w-[720px] px-4 sm:px-6 lg:px-8">
        <h2 className="font-[family-name:var(--font-serif)] text-[2.2rem] leading-[1.1] tracking-[-0.03em] sm:text-[2.6rem]">
          {d.title}
        </h2>
        <p className="mt-4 max-w-[560px] text-[15px] leading-7 text-[#0a0d12]/72">
          {d.sub}
        </p>

        <div className="mt-10">
          <CloudWaitlistExpand
            submitted={submitted}
            onSubmitted={() => setSubmitted(true)}
          />
        </div>
      </div>
    </section>
  );
}
