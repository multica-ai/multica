"use client";
import { use } from "react";
import { EmbeddedLinkPage } from "@multica/views/links";
export default function LinkPage({params}: {params: Promise<{id: string}>}) {
  const {id} = use(params);
  return <EmbeddedLinkPage id={id} />;
}
