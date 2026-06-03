import { redirect } from "next/navigation";

/**
 * 3J Tracker root redirect.
 *
 * The upstream Multica marketing landing page is replaced with a direct
 * redirect to /login.  RedirectIfAuthenticated lives on the login page itself,
 * so authenticated users are forwarded to their workspace automatically.
 */
export default function RootPage() {
  redirect("/login");
}
