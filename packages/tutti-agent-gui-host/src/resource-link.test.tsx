// @vitest-environment jsdom
import { act } from "react";
import { createRoot } from "react-dom/client";
import { expect, it, vi } from "vitest";
import { AgentMessageMarkdown } from "../../../vendor/tutti/packages/agent/gui/shared/AgentMessageMarkdown";
import { fakeHostServices } from "../e2e-harness/fake-host-services";
import { ResourceLinkProvider, ResourceMarkdownLink } from "./resource-link";
import { SideEntryProvider } from "./side-entry";

it.each([
  { streaming: false, sidePane: false },
  { streaming: true, sidePane: false },
  { streaming: false, sidePane: true },
  { streaming: true, sidePane: true },
])("renders only explicit message targets ($streaming streaming, $sidePane side)", async ({ streaming, sidePane }) => {
  vi.stubGlobal("IS_REACT_ACT_ENVIRONMENT", true);
  const request = vi.fn().mockResolvedValue(undefined);
  const client = fakeHostServices(request);
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  const content = [
    "`AGENTS.md` and `src/example.ts` and ./output/report.html",
    "[relative label](./AGENTS.md) and [`relative code`](src/example.ts)",
    "[reference][relative] and [no extension](docs/readme)",
    "[AGENTS.md](</project/房间 report.md:12>)",
    "[absolute reference][absolute] and [website](https://example.org/docs)",
    "[CHOV-60](mention://issue/CHOV-60)",
    "",
    "[relative]: ../AGENTS.md",
    "[absolute]: /project/absolute.md",
  ].join("\n\n");
  try {
    await act(async () => root.render(
      <ResourceLinkProvider client={client} scope={{ workspaceId: "workspace", workspaceSlug: "chovy-multica" }} agents={[]}>
        <SideEntryProvider root={container} client={client} handle={{} as Parameters<typeof SideEntryProvider>[0]["handle"]} sessionId="session-a" chinese enabled sidePane={sidePane}>
          <AgentMessageMarkdown content={content} streaming={streaming} />
          <ResourceMarkdownLink href="AGENTS.md" fallback={<a href="AGENTS.md">fallback</a>}>
            <code>direct relative</code>
          </ResourceMarkdownLink>
        </SideEntryProvider>
      </ResourceLinkProvider>
    ));
    const links = [...container.querySelectorAll("a")];
    // Streaming blocks resolve references only after the complete document settles.
    expect(links.map((link) => link.textContent)).toEqual(streaming
      ? ["AGENTS.md", "website", "CHOV-60"]
      : ["AGENTS.md", "absolute reference", "website", "CHOV-60"]);
    expect(links[0]!.getAttribute("href")).toBe(encodeURI("/project/房间 report.md:12"));
    expect([...container.querySelectorAll("code")].map((code) => code.textContent)).toEqual(["AGENTS.md", "src/example.ts", "relative code", "direct relative"]);
    expect(container.textContent).toContain("relative label");
    expect(container.textContent).toContain("no extension");
    expect(container.textContent).toContain("reference");
    await act(async () => links[0]!.click());
    expect(request).toHaveBeenCalledWith("host.openResource", { sessionId: "session-a", url: encodeURI("/project/房间 report.md:12") });
    request.mockClear();
    await act(async () => links.at(-1)!.click());
    expect(request).toHaveBeenCalledExactlyOnceWith("host.openInternalResource", { href: "/chovy-multica/issues/CHOV-60", background: false });
  } finally {
    await act(async () => root.unmount());
    container.remove();
    vi.unstubAllGlobals();
  }
});
