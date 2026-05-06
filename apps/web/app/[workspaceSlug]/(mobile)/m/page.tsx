import { redirect } from "next/navigation";
import { mobileRoutes } from "@/features/mobile/mobile-routes";

export default async function Page({
  params,
}: {
  params: Promise<{ workspaceSlug: string }>;
}) {
  const { workspaceSlug } = await params;
  redirect(mobileRoutes(workspaceSlug).root);
}
