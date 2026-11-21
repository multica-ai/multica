import { render, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MemoryRouter } from "react-router-dom";
import { beforeEach, describe, expect, it } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { SupportedLocale } from "@multica/core/i18n";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { workspaceListOptions } from "@multica/core/workspace";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { RESOURCES } from "@multica/views/locales";
import { DocumentTitleSync } from "./document-title-sync";

function makeClient() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(workspaceListOptions().queryKey, [
    { id: "ws1", slug: "acme" },
  ] as never);
  return qc;
}

function tree(qc: QueryClient, locale: SupportedLocale, url: string) {
  return (
    <I18nProvider locale={locale} resources={RESOURCES}>
      <QueryClientProvider client={qc}>
        <WorkspaceSlugProvider slug="acme">
          <MemoryRouter initialEntries={[url]}>
            <DocumentTitleSync />
          </MemoryRouter>
        </WorkspaceSlugProvider>
      </QueryClientProvider>
    </I18nProvider>
  );
}

beforeEach(() => {
  document.title = "";
});

describe("DocumentTitleSync", () => {
  it("writes the localized page title and follows a language switch", async () => {
    const qc = makeClient();
    const { rerender } = render(tree(qc, "zh-Hans", "/acme/projects"));
    await waitFor(() => expect(document.title).toBe("项目"));

    rerender(tree(qc, "ja", "/acme/projects"));
    await waitFor(() => expect(document.title).toBe("プロジェクト"));
  });

  it("uses the localized type label while a resource loads, then its name", async () => {
    const qc = makeClient();
    render(tree(qc, "zh-Hans", "/acme/issues/MUL-1"));
    await waitFor(() => expect(document.title).toBe("任务"));

    qc.setQueryData(issueDetailOptions("ws1", "MUL-1").queryKey, {
      id: "i1",
      identifier: "MUL-1",
      title: "Fix login",
      status: "todo",
    } as never);
    await waitFor(() => expect(document.title).toBe("MUL-1: Fix login"));
  });

  it("localizes creation flows", async () => {
    render(tree(makeClient(), "zh-Hans", "/acme/agents/new/manual"));
    await waitFor(() => expect(document.title).toBe("创建智能体"));
  });
});
