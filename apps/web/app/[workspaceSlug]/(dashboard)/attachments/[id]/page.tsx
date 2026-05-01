"use client";

import { useParams } from "next/navigation";
import { AttachmentViewPage } from "@multica/views/attachments/pages";

export default function Page() {
  const params = useParams<{ id: string }>();
  return <AttachmentViewPage attachmentId={params.id} />;
}
