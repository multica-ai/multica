"use client";
import { use } from "react";
import { DocPage } from "@multica/views/docs";
export default function DocRoute({params}: {params: Promise<{id: string}>}) { return <DocPage docId={use(params).id} />; }
