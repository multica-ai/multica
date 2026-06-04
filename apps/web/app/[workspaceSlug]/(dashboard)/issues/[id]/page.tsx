import { headers } from "next/headers";
import { MobileAppOpenBanner } from "@/components/mobile-app-open-banner";
import { getRequestLocale } from "@/lib/request-locale";
import {
  buildIssueMobileAppHref,
  isMobileUserAgent,
  isWeChatUserAgent,
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
  const userAgent = headerList.get("user-agent");
  const isMobileRequest = isMobileUserAgent(userAgent);
  const isWeChatRequest = isWeChatUserAgent(userAgent);
  const issueHref = isMobileRequest && !isWeChatRequest
    ? buildIssueMobileAppHref({
        workspaceSlug,
        issueId: id,
        searchParams: query,
      })
    : null;
  const mobileBanner = !isMobileRequest ? null : isWeChatRequest ? (
    <MobileAppOpenBanner locale={locale} mode="wechat" />
  ) : issueHref ? (
    <MobileAppOpenBanner href={issueHref} locale={locale} mode="open-app" />
  ) : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {mobileBanner}
      <IssueDetailClient issueId={id} />
    </div>
  );
}
