import { act, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { OneShotRunPage } from "./one-shot-run-page";
import common from "../locales/en/common.json";

vi.mock("@multica/core/api", () => ({ api: { getOneShotRun: vi.fn(), listTaskMessages: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace" }));
vi.mock("../i18n", () => ({ useT: () => ({ t: (selector: (value: typeof common) => string) => selector(common) }) }));
vi.mock("../common/task-transcript", async (importOriginal) => {
  const original = await importOriginal<typeof import("../common/task-transcript")>();
  return { ...original, AgentTranscriptDialog: ({ items }: { items: unknown[] }) => <div data-testid="transcript">{JSON.stringify(items)}</div> };
});
afterEach(() => vi.clearAllMocks());

it("retrieves the final transcript tail when a running task completes", async () => {
  const running = { task: { id: "run", status: "running" }, input: { user_prompt: "Review" } };
  vi.mocked(api.getOneShotRun).mockResolvedValue(running as Awaited<ReturnType<typeof api.getOneShotRun>>);
  let resolveTail!: (messages: Awaited<ReturnType<typeof api.listTaskMessages>>) => void;
  const finalTail = new Promise<Awaited<ReturnType<typeof api.listTaskMessages>>>((resolve) => { resolveTail = resolve; });
  vi.mocked(api.listTaskMessages).mockResolvedValueOnce([{ seq: 1, type: "text", content: "Earlier output" }] as Awaited<ReturnType<typeof api.listTaskMessages>>).mockReturnValueOnce(finalTail);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const view = render(<QueryClientProvider client={client}><OneShotRunPage taskId="run" /></QueryClientProvider>);
  await waitFor(() => expect(screen.getByTestId("transcript").textContent).toContain("Earlier output"));
  act(() => client.setQueryData(["oneshot-run", "workspace", "run"], { ...running, task: { ...running.task, status: "completed" } }));
  await waitFor(() => expect(api.listTaskMessages).toHaveBeenCalledTimes(2));
  expect(screen.getByTestId("transcript").textContent).toContain("Earlier output");
  act(() => resolveTail([{ seq: 1, type: "text", content: "Earlier output" }, { seq: 2, type: "text", content: "Final result" }] as Awaited<ReturnType<typeof api.listTaskMessages>>));
  await waitFor(() => expect(screen.getByTestId("transcript").textContent).toContain("Final result"));
  expect(api.listTaskMessages).toHaveBeenCalledTimes(2);
  view.unmount(); client.clear();
});
