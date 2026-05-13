import { NextResponse } from "next/server";
import { resolveReleaseRepository } from "@/features/landing/utils/github-release";

export function GET() {
  const repository = resolveReleaseRepository();
  return NextResponse.redirect(
    `https://raw.githubusercontent.com/${repository}/main/scripts/install.ps1`,
  );
}
