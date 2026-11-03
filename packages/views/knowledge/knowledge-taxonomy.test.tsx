// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { KnowledgeBase, KnowledgeKind, KnowledgePublishPolicies } from "@multica/core/knowledge";
import { renderWithI18n } from "../test/i18n";
import { AddEvalCaseButton } from "./knowledge-add-eval-case";
import { KnowledgeKindPicker } from "./knowledge-kind-picker";
import { KnowledgePolicyList } from "./knowledge-policy-list";

vi.mock("@multica/core/api", () => ({
  api: {
    createKnowledgeEvalCase: vi.fn(),
    listKnowledgePolicies: vi.fn(),
    setKnowledgePolicy: vi.fn(),
    deleteKnowledgePolicy: vi.fn(),
  },
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const bases: KnowledgeBase[] = [
  { id: "b1", key: "engineering", name: "Engineering", visibility: "workspace", position: 0 } as KnowledgeBase,
];
const kinds: KnowledgeKind[] = [
  { key: "adr", name: "Architecture decision", default_base_key: "engineering", template: "", review_days: 0, required_sections: [] },
];

function renderWith(ui: React.ReactElement) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("AddEvalCaseButton", () => {
  it("prefills the question, saves the case against the hit's document, and closes", async () => {
    vi.mocked(api.createKnowledgeEvalCase).mockResolvedValue({
      id: "c1", question: "queues", expected_kind: "", expected_document_id: "doc-7",
      owner_squad_id: null, created_by_type: "member", created_at: "",
    });
    renderWith(
      <AddEvalCaseButton wsId="ws-1" question="queues" documentId="doc-7" documentTitle="Storage" />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Add as eval case" }));
    const input = await screen.findByLabelText("Question");
    expect(input).toHaveValue("queues");

    fireEvent.click(screen.getByRole("button", { name: "Add case" }));
    await waitFor(() =>
      expect(api.createKnowledgeEvalCase).toHaveBeenCalledWith({ question: "queues", expected_document_id: "doc-7" }),
    );
    await waitFor(() => expect(screen.queryByLabelText("Question")).not.toBeInTheDocument());
    expect(toast.success).toHaveBeenCalledWith("Eval case added");
  });

  it("keeps the dialog open and the question when the save fails", async () => {
    vi.mocked(api.createKnowledgeEvalCase).mockRejectedValue(new Error("boom"));
    renderWith(<AddEvalCaseButton wsId="ws-1" question="queues" documentId="doc-7" documentTitle="Storage" />);

    fireEvent.click(screen.getByRole("button", { name: "Add as eval case" }));
    fireEvent.click(await screen.findByRole("button", { name: "Add case" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Could not add the eval case"));
    expect(screen.getByLabelText("Question")).toHaveValue("queues");
  });
});

describe("KnowledgeKindPicker", () => {
  it("offers all kinds first and reports the chosen key", () => {
    const onChange = vi.fn();
    renderWith(<KnowledgeKindPicker kinds={kinds} value="" onValueChange={onChange} />);
    expect(screen.getByRole("combobox", { name: "Kind" })).toBeInTheDocument();
    expect(screen.getByText("All kinds")).toBeInTheDocument();
  });
});

describe("KnowledgePolicyList", () => {
  it("shows every base and kind, with manual as the default for scopes without a row", async () => {
    vi.mocked(api.listKnowledgePolicies).mockResolvedValue({
      policies: [{ scope: "base", key: "engineering", mode: "auto", updated_at: "" }],
      default_mode: "manual",
    } satisfies KnowledgePublishPolicies);
    renderWith(<KnowledgePolicyList wsId="ws-1" knowledgeBases={bases} kinds={kinds} />);

    expect(await screen.findByText("Architecture decision")).toBeInTheDocument();
    expect(screen.getByText("Engineering")).toBeInTheDocument();
    // The adr kind has no row, so it reads as using the default.
    expect(screen.getByText(/^Kind · using the default$/)).toBeInTheDocument();
  });

  it("removes a policy with a reset", async () => {
    vi.mocked(api.listKnowledgePolicies).mockResolvedValue({
      policies: [{ scope: "kind", key: "adr", mode: "auto_after_review", updated_at: "" }],
      default_mode: "manual",
    });
    vi.mocked(api.deleteKnowledgePolicy).mockResolvedValue(undefined);
    renderWith(<KnowledgePolicyList wsId="ws-1" knowledgeBases={bases} kinds={kinds} />);

    fireEvent.click(await screen.findByRole("button", { name: "Reset" }));
    await waitFor(() => expect(api.deleteKnowledgePolicy).toHaveBeenCalledWith("kind", "adr"));
  });

  it("degrades a mode it cannot read to unknown rather than offering a control for it", async () => {
    vi.mocked(api.listKnowledgePolicies).mockResolvedValue({
      policies: [{ scope: "base", key: "engineering", mode: "unknown", updated_at: "" }],
      default_mode: "manual",
    });
    renderWith(<KnowledgePolicyList wsId="ws-1" knowledgeBases={bases} kinds={kinds} />);

    expect(await screen.findByText("Unknown mode")).toBeInTheDocument();
  });
});
