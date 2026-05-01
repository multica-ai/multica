const INSTALL_SCRIPT_URL =
  "https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh";

export async function GET() {
  const upstreamResponse = await fetch(INSTALL_SCRIPT_URL, {
    headers: {
      accept: "text/x-shellscript,text/plain,*/*",
    },
    next: {
      revalidate: 300,
    },
  });

  if (!upstreamResponse.ok) {
    return new Response(
      `Failed to fetch Multica installer from ${INSTALL_SCRIPT_URL}\n`,
      {
        status: 502,
        headers: {
          "content-type": "text/plain; charset=utf-8",
          "cache-control": "no-store",
        },
      },
    );
  }

  const script = await upstreamResponse.text();

  return new Response(script, {
    headers: {
      "content-type": "text/x-shellscript; charset=utf-8",
      "cache-control": "public, max-age=300, s-maxage=300",
    },
  });
}
