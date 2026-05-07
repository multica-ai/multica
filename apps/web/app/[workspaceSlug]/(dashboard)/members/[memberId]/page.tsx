import { MemberDetailPage } from "@multica/cerebro-users/views";

export default async function Page({
  params,
}: {
  params: Promise<{ memberId: string }>;
}) {
  const { memberId } = await params;
  return <MemberDetailPage memberId={memberId} />;
}
