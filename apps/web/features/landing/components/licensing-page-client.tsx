"use client";

import { cn } from "@multica/ui/lib/utils";
import { DocumentPage, DocumentSections } from "./document-page";
import { useLocale } from "../i18n";

export function LicensingPageClient() {
  const { t } = useLocale();
  const l = t.licensing;

  return (
    <DocumentPage title={l.title} intro={l.intro}>
      <aside className="mt-12 rounded-2xl bg-[#f8f8f8] p-6 sm:p-8">
        <p className="text-micro font-semibold uppercase tracking-[0.16em] text-[#0a0d12]/40">
          {l.rule.title}
        </p>
        <p className="mt-3 text-title-sm leading-[1.7] text-[#0a0d12]">
          {l.rule.text}
        </p>
      </aside>

      <section className="mt-12">
        <h2 className="text-title font-semibold leading-snug text-[#0a0d12]">
          {l.scenarios.title}
        </h2>
        <table className="mt-6 w-full border-collapse text-left">
          <thead>
            <tr className="border-b border-[#0a0d12]/10 text-caption font-semibold uppercase tracking-[0.1em] text-[#0a0d12]/40">
              <th scope="col" className="pb-3 pr-4 font-semibold">
                {l.scenarios.scenarioColumn}
              </th>
              <th
                scope="col"
                className="whitespace-nowrap pb-3 text-right font-semibold"
              >
                {l.scenarios.licenseColumn}
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-[#0a0d12]/10">
            {l.scenarios.items.map((item) => (
              <tr key={item.scenario} className="align-top">
                <td className="py-4 pr-4">
                  <p className="text-body-lg font-medium leading-[1.6] text-[#0a0d12]">
                    {item.scenario}
                  </p>
                  {item.example && (
                    <p className="mt-1 text-body leading-[1.6] text-[#0a0d12]/50">
                      {item.example}
                    </p>
                  )}
                </td>
                <td className="py-4 text-right">
                  <span
                    className={cn(
                      "inline-flex whitespace-nowrap rounded-full px-2.5 py-1 text-caption font-semibold",
                      item.required
                        ? "bg-[#0a0d12] text-white"
                        : "border border-[#0a0d12]/12 text-[#0a0d12]/60",
                    )}
                  >
                    {item.required
                      ? l.scenarios.required
                      : l.scenarios.notRequired}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <DocumentSections sections={l.sections} />
    </DocumentPage>
  );
}
