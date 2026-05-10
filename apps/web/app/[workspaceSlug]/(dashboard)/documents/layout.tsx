"use client";

import { useParams } from "next/navigation";
import { DocumentsShell } from "@multica/views/documents";

// Persists the documents page header and tree sidebar across /documents and
// /documents/[id]. Next.js keeps this layout mounted when navigating between
// the two routes, so the sidebar (and its expand/search state) doesn't
// reset every time the user picks a document.
export default function DocumentsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const params = useParams();
  const id = typeof params?.id === "string" ? params.id : null;
  return <DocumentsShell selectedId={id}>{children}</DocumentsShell>;
}
