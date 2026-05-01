// Landing pages are not public-facing for this internal deployment.
// The download page is the only exception — it's linked from onboarding.
export default function LandingLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  // Render landing children (only /download is reachable from onboarding)
  return <>{children}</>;
}
