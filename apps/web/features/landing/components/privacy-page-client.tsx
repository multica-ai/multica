"use client";

import { DocumentPage, DocumentSections } from "./document-page";
import { useLocale } from "../i18n";

export function PrivacyPageClient() {
  const { t } = useLocale();
  const p = t.privacy;

  return (
    <DocumentPage title={p.title} meta={p.lastUpdated} intro={p.intro}>
      <DocumentSections sections={p.sections} />
    </DocumentPage>
  );
}
