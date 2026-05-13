"use client";

import { useQuery } from "@tanstack/react-query";
import { Mail, Search } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { crmEmailThreadListOptions } from "@multica/core/crm/queries";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";
import { useT } from "../../i18n";

export function CRMEmailsPage() {
  const wsId = useWorkspaceId();
  const { t } = useT("crm");
  const { data: threads = [], isLoading } = useQuery(crmEmailThreadListOptions(wsId));

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
          {isLoading ? (
            <section className="space-y-2 rounded-lg border bg-card p-4">
              <Skeleton className="h-14 w-full" />
              <Skeleton className="h-14 w-full" />
            </section>
          ) : threads.length === 0 ? (
            <section className="rounded-lg border bg-card p-10 text-center">
              <div className="mx-auto flex size-12 items-center justify-center rounded-full bg-primary/10 text-primary">
                <Mail className="size-5" />
              </div>
              <h2 className="mt-4 text-base font-semibold">{t(($) => $.emails.empty_title)}</h2>
              <p className="mx-auto mt-2 max-w-xl text-sm text-muted-foreground">
                {t(($) => $.emails.empty_description)}
              </p>
            </section>
          ) : (
            <section className="divide-y rounded-lg border bg-card">
              {threads.map((thread) => (
                <article key={thread.id} className="px-4 py-3 text-sm">
                  <div className="font-medium">{thread.subject}</div>
                  <div className="mt-1 text-xs text-muted-foreground">
                    {[thread.mailbox, thread.direction, thread.status, t(($) => $.common.count_messages, { count: thread.message_count })].filter(Boolean).join(" · ")}
                  </div>
                </article>
              ))}
            </section>
          )}
        </div>
      </div>
    </div>
  );
}
