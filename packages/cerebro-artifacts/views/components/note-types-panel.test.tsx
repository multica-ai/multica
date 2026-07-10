import { describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const createNoteTypeMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
const updateNoteTypeMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue(undefined),
);
const deleteNoteTypeMutate = vi.hoisted(() => vi.fn());
const runNoteTypeMutateAsync = vi.hoisted(() =>
  vi.fn().mockResolvedValue({ artifact_id: "artifact-1", created: true }),
);
const navigationPush = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    notes: () => "/ws/notes",
  }),
}));

vi.mock("@multica/views/navigation", () => ({
  useNavigation: () => ({
    push: navigationPush,
  }),
}));

vi.mock("@multica/cerebro-artifacts/core", () => ({
  artifactFoldersOptions: () => ({
    queryKey: ["artifact-folders", "note"],
    queryFn: () => Promise.resolve([]),
  }),
  useNoteTypes: () => ({
    data: [],
    isLoading: false,
  }),
  useCreateNoteType: () => ({
    mutateAsync: createNoteTypeMutateAsync,
    isPending: false,
  }),
  useUpdateNoteType: () => ({
    mutateAsync: updateNoteTypeMutateAsync,
    isPending: false,
  }),
  useDeleteNoteType: () => ({
    mutate: deleteNoteTypeMutate,
    isPending: false,
  }),
  useRunNoteType: () => ({
    mutateAsync: runNoteTypeMutateAsync,
    isPending: false,
  }),
}));

import { NoteTypesPanel } from "./note-types-panel";

function renderPanel() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData(["artifact-folders", "note"], []);
  return render(
    <QueryClientProvider client={qc}>
      <NoteTypesPanel />
    </QueryClientProvider>,
  );
}

describe("NoteTypesPanel", () => {
  it("renders the recurring note type editor labels in English", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.click(screen.getByRole("button", { name: /new note type/i }));

    expect(screen.getByRole("button", { name: /back/i })).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "New note type" }),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveAttribute(
      "placeholder",
      "E.g. Monthly Business Review",
    );
    expect(screen.getByLabelText("Template (inserted every time)")).toBeInTheDocument();
    expect(screen.getByText("How does it repeat?")).toBeInTheDocument();
    expect(
      screen.getByText("A new template is added at the top of the same document."),
    ).toBeInTheDocument();
    expect(screen.getByText("Frequency")).toBeInTheDocument();
    expect(screen.getByText("Every")).toBeInTheDocument();
    expect(screen.getByText("Runs automatically every month.")).toBeInTheDocument();
    expect(screen.getByText("Folder")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByText("Off: does not run automatically.")).toBeInTheDocument();
    expect(screen.getByText("Auto-number")).toBeInTheDocument();
    expect(
      screen.getByText("Each new note gets a sequential number (#1, #2, #3 ...)."),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Cancel" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save note type" })).toBeInTheDocument();
  });

  it("renders recurring note dropdown values with user-facing English labels", async () => {
    const user = userEvent.setup();
    renderPanel();

    await user.click(screen.getByRole("button", { name: /new note type/i }));

    await user.click(screen.getByText("Running document"));
    const recurrenceListbox = await screen.findByRole("listbox");
    expect(within(recurrenceListbox).getByText("Running document")).toBeInTheDocument();
    expect(within(recurrenceListbox).getByText("New note each period")).toBeInTheDocument();
    expect(screen.queryByText("running_doc")).not.toBeInTheDocument();

    await user.keyboard("{Escape}");
    await user.click(screen.getByText("Month"));
    const cadenceListbox = await screen.findByRole("listbox");
    for (const label of ["Manual", "Day", "Week", "Month", "Quarter"]) {
      expect(within(cadenceListbox).getByText(label)).toBeInTheDocument();
    }
    expect(screen.queryByText("Måned")).not.toBeInTheDocument();

    await user.keyboard("{Escape}");
    await user.click(screen.getByText("None"));
    const folderListbox = await screen.findByRole("listbox");
    expect(within(folderListbox).getByText("None")).toBeInTheDocument();
    expect(screen.queryByText("__none__")).not.toBeInTheDocument();
  });
});
