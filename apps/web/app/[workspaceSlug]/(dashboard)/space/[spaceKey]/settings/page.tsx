import { redirect } from "next/navigation";
import { paths } from "@multica/core/paths";

export default async function Page({
  params,
}: {
  params: Promise<{ workspaceSlug: string; spaceKey: string }>;
}) {
  const { workspaceSlug, spaceKey } = await params;
  redirect(paths.workspace(workspaceSlug).spaceSettings(spaceKey));
}
