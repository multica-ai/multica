// @vitest-environment node
import { expect, it, vi } from "vitest";
import { renderToString } from "react-dom/server";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ContentEditor } from "./content-editor";

vi.mock("../i18n", () => ({ useT: () => ({ t: () => "" }) }));

it("keeps the null server snapshot without creating an editor", () => {
  const onReady = vi.fn();
  const html = renderToString(<QueryClientProvider client={new QueryClient()}><ContentEditor value="Server description." onReady={onReady} /></QueryClientProvider>);
  expect(html).toBe("");
  expect(onReady).not.toHaveBeenCalled();
});
