"use client";

import { use } from "react";
import { MemberDetailPage } from "@wallts/views/members";

export default function MemberDetailRoute({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = use(params);
  return <MemberDetailPage userId={id} />;
}
