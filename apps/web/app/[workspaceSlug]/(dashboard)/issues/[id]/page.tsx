import { headers } from "next/headers";
import { MobileAppOpenBanner } from "@/components/mobile-app-open-banner";
import { getRequestLocale } from "@/lib/request-locale";
import {
  buildIssueWebHref,
  isMobileUserAgent,
  type SearchParams,
} from "@/lib/mobile-web-link";
import { IssueDetailClient } from "./issue-detail-client";

export default async function IssueDetailPage({
  params,
  searchParams,
}: {
  params: Promise<{ workspaceSlug: string; id: string }>;
  searchParams: Promise<SearchParams>;
}) {
  const [{ workspaceSlug, id }, query, headerList, locale] = await Promise.all([
    params,
    searchParams,
    headers(),
    getRequestLocale(),
  ]);
  const isMobileRequest = isMobileUserAgent(headerList.get("user-agent"));
  const issueHref = isMobileRequest
    ? buildIssueWebHref({
        headers: headerList,
        workspaceSlug,
        issueId: id,
        searchParams: query,
      })
    : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {issueHref ? <MobileAppOpenBanner href={issueHref} locale={locale} /> : null}
      <IssueDetailClient issueId={id} />
    </div>
  );
}
