import { readFile } from "node:fs/promises";

export const runtime = "nodejs";

export async function GET() {
  const script = await readFile(
    new URL("../../../../scripts/install.sh", import.meta.url),
    "utf8",
  );

  return new Response(script, {
    headers: {
      "Cache-Control": "public, max-age=300",
      "Content-Disposition": 'inline; filename="install.sh"',
      "Content-Type": "text/x-shellscript; charset=utf-8",
    },
  });
}
