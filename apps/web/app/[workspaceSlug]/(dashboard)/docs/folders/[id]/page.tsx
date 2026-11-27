"use client";
import { use } from "react";
import { DocsHomePage } from "@multica/views/docs";
export default function FolderRoute({params}: {params: Promise<{id: string}>}) { return <DocsHomePage folderId={use(params).id} />; }
