import { cookies } from "next/headers";
import { redirect } from "next/navigation";

export default async function LandingPage() {
  const cookieStore = await cookies();
  const lastWorkspaceSlug = cookieStore.get("last_workspace_slug")?.value;

  if (lastWorkspaceSlug) {
    redirect(`/${lastWorkspaceSlug}/issues`);
  }

  redirect("/login");
}
