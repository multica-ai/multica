import { MemberDetailPage } from "@multica/views/members";

export default async function Page({
  params,
}: {
  params: Promise<{ memberId: string }>;
}) {
  const { memberId } = await params;
  return <MemberDetailPage memberId={memberId} />;
}
