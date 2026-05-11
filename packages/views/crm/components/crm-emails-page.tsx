"use client";

import { Mail, Search } from "lucide-react";
import { Input } from "@multica/ui/components/ui/input";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";

export function CRMEmailsPage() {
  const { t } = useT("crm");

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Mail className="size-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">{t(($) => $.emails.title)}</h1>
        </div>
      </PageHeader>

      <div className="flex min-h-0 flex-1 flex-col p-6">
        <div className="mx-auto flex w-full max-w-4xl flex-col gap-4">
          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 size-4 text-muted-foreground" />
            <Input className="pl-8" placeholder={t(($) => $.emails.search_placeholder)} disabled />
          </div>
          <section className="rounded-lg border bg-card p-10 text-center">
            <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary">
              <Mail className="size-5" />
            </div>
            <h2 className="mt-4 text-base font-semibold">{t(($) => $.emails.empty_title)}</h2>
            <p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">
              {t(($) => $.emails.empty_description)}
            </p>
          </section>
        </div>
      </div>
    </div>
  );
}
