import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError, setApiInstance } from "@multica/core/api";
import { I18nProvider } from "@multica/core/i18n/react";
import type {
  PlatformExtensionDetail,
  PlatformExtensionImportResult,
  PlatformExtensionMapping,
  PlatformExtensionPreview,
} from "@multica/core/extensions";
import enExtensions from "../locales/en/extensions.json";
import { ExtensionsPage } from "./extensions-page";

const runtime = {
  id: "22222222-2222-4222-8222-222222222222",
  provider: "platform-agent-cli" as const,
  name: "My Platform Agent CLI",
};

const mapping: PlatformExtensionMapping = {
  release: {
    id: "11111111-1111-4111-8111-111111111111",
    extension_key: "delegate",
    version: "1.0.0",
    digest: `sha256:${"a".repeat(64)}`,
  },
  runtime,
  squad: { id: "33333333-3333-4333-8333-333333333333", name: "delegate · v1.0.0" },
  agents: [{
    source_key: "lead",
    id: "44444444-4444-4444-8444-444444444444",
    name: "Delegate Lead",
    leader: true,
    runtime,
  }],
  skills: [{
    source_key: "command:evidence",
    id: "55555555-5555-4555-8555-555555555555",
    name: "delegate · v1.0.0 / evidence",
  }],
};

const detail: PlatformExtensionDetail = {
  ...mapping,
  available_runtimes: [runtime],
  manifest: {
    agents: [{
      key: "lead",
      name: "Delegate Lead",
      prompt: "# Delegate Lead\n\nCoordinate the team.",
    }],
    flow_commands: [{ name: "delegate-e2e" }],
    runtime_commands: [{ name: "evidence" }],
    skills: [{
      key: "handoff",
      name: "handoff",
      files: [
        { path: "SKILL.md", content: "# Handoff\n\nPass work to another agent." },
        { path: "references/checklist.md", content: "- Preserve task context" },
        { path: "assets/logo.bin", content: "AP8=", encoding: "base64" },
      ],
    }],
  },
};

const preview: PlatformExtensionPreview = {
  release: {
    extension_key: "delegate",
    version: "1.1.0",
    digest: `sha256:${"b".repeat(64)}`,
  },
  squad_base_name: "delegate",
  agents: [{ source_key: "lead", name: "Delegate Lead", leader: true, runtime_id: runtime.id }],
  runtimes: [runtime],
  manifest: detail.manifest,
};

function renderPage() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale="en" resources={{ en: { extensions: enExtensions } }}>
        <ExtensionsPage wsId="ws-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function fileWithBytes(bytes: Uint8Array, name = "extension.zip") {
  const file = new File([Uint8Array.from(bytes).buffer], name, { type: "application/zip" });
  Object.defineProperty(file, "arrayBuffer", {
    value: vi.fn().mockResolvedValue(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength)),
  });
  return file;
}

let apiClient: ApiClient;

beforeEach(() => {
  apiClient = new ApiClient("https://api.example.test");
  setApiInstance(apiClient);
  vi.spyOn(apiClient, "listPlatformExtensions").mockResolvedValue([mapping]);
  vi.spyOn(apiClient, "getPlatformExtension").mockResolvedValue(detail);
  vi.spyOn(apiClient, "previewPlatformExtension").mockResolvedValue(preview);
  vi.spyOn(apiClient, "importPlatformExtension").mockResolvedValue({
    ...mapping,
    idempotent: false,
  } satisfies PlatformExtensionImportResult);
});

afterEach(() => {
  window.sessionStorage.clear();
  vi.restoreAllMocks();
});

describe("ExtensionsPage", () => {
  it("keeps imported Agent and Skills inside the Squad resource inventory", async () => {
    renderPage();
    expect(await screen.findByTestId("extension-history")).toBeInTheDocument();
    expect(await screen.findByTestId("extension-atomic-mapping")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Resource inventory" }));
    expect(await screen.findByText("下列 Agent 与 Skills 都是该小队的内部资源，仅在小队编排与执行时生效，不会出现在全局“智能体”或“Skills”列表，也不能作为普通任务的直接分配对象。")).toBeInTheDocument();
    expect(screen.getByText("Delegate Lead")).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: /Delegate Lead/ })).not.toBeInTheDocument();
  });

  it("opens an internal Agent source file from the resource inventory", async () => {
    renderPage();

    fireEvent.click(await screen.findByRole("tab", { name: "Resource inventory" }));
    fireEvent.click(await screen.findByRole("button", { name: "View source for Delegate Lead" }));

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "agents/lead.md" })).toBeInTheDocument();
    expect(screen.getByTestId("extension-source-content").textContent).toBe("# Delegate Lead\n\nCoordinate the team.");
  });

  it("shows every Skill file read-only and labels binary assets without rendering them as text", async () => {
    renderPage();

    fireEvent.click(await screen.findByRole("tab", { name: "Resource inventory" }));
    fireEvent.click(await screen.findByRole("button", { name: "View source for handoff" }));

    const dialog = await screen.findByRole("dialog");
    expect(dialog.className).toContain("sm:max-w-[1800px]");
    expect(screen.getByRole("button", { name: "Toggle folder skills" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: "Toggle folder handoff" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: "Toggle folder references" })).toHaveAttribute("aria-expanded", "true");
    expect(await screen.findByRole("button", { name: "skills/handoff/SKILL.md" })).toBeInTheDocument();
    expect(screen.getByTestId("extension-source-content").textContent).toBe("# Handoff\n\nPass work to another agent.");

    fireEvent.click(screen.getByRole("button", { name: "Toggle folder references" }));
    expect(screen.queryByRole("button", { name: "skills/handoff/references/checklist.md" })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Toggle folder references" }));
    fireEvent.click(screen.getByRole("button", { name: "skills/handoff/references/checklist.md" }));
    expect(screen.getByTestId("extension-source-content").textContent).toBe("- Preserve task context");

    fireEvent.click(screen.getByRole("button", { name: "skills/handoff/assets/logo.bin" }));
    expect(screen.getByTestId("extension-binary-file")).toHaveTextContent("二进制文件");
    expect(screen.getByTestId("extension-binary-file")).toHaveTextContent("2 B");
  });

  it("explains that changed content for an existing version needs a version bump", async () => {
    vi.mocked(apiClient.previewPlatformExtension).mockResolvedValue({
      ...preview,
      release: { ...preview.release, version: "1.0.0" },
    });
    vi.mocked(apiClient.importPlatformExtension).mockRejectedValue(new ApiError(
      "extension version is immutable",
      409,
      "Conflict",
      { code: "EXTENSION_VERSION_IMMUTABLE" },
    ));
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(new Uint8Array([0x50, 0x4b, 0x03, 0x04]))] },
    });
    fireEvent.click(await screen.findByRole("button", { name: "确认导入" }));

    const conflict = await screen.findByText("This Squad template has been copied. To preserve version consistency, update version in codeagent-extension.json and import again.");
    expect(conflict).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "取消" }));

    await waitFor(() => expect(screen.queryByText("This Squad template has been copied. To preserve version consistency, update version in codeagent-extension.json and import again.")).not.toBeInTheDocument());
    expect(screen.getByText("01")).toBeInTheDocument();
    expect((await screen.findAllByText("Delegate Lead")).length).toBeGreaterThan(0);
  });

  it("keeps an archived Extension mapping visible but read-only", async () => {
    const archivedMapping = {
      ...mapping,
      squad: { ...mapping.squad, archived: true },
    };
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([archivedMapping]);
    vi.mocked(apiClient.getPlatformExtension).mockResolvedValue({
      ...detail,
      squad: { ...detail.squad, archived: true },
    });

    renderPage();

    expect(await screen.findByTestId("extension-atomic-mapping")).toBeInTheDocument();
    expect(screen.getAllByText("已归档")).toHaveLength(3);
    expect(screen.getByLabelText("小队名称")).toBeDisabled();
    expect(screen.getByLabelText("Delegate Lead runtime")).toBeDisabled();
    expect(screen.queryByRole("button", { name: /归档/ })).not.toBeInTheDocument();
    expect(screen.getByText("delegate-e2e")).toBeInTheDocument();
    expect(screen.getAllByText("evidence").length).toBeGreaterThan(0);
  });

  it("explains that an archived version must use a new version before import", async () => {
    vi.mocked(apiClient.previewPlatformExtension).mockResolvedValue({
      ...preview,
      release: { ...preview.release, version: "1.0.0" },
    });
    vi.mocked(apiClient.importPlatformExtension).mockRejectedValue(new ApiError(
      "extension version is archived",
      409,
      "Conflict",
      { code: "EXTENSION_VERSION_ARCHIVED" },
    ));
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(new Uint8Array([0x50, 0x4b, 0x03, 0x04]))] },
    });
    fireEvent.click(await screen.findByRole("button", { name: "确认导入" }));

    expect(await screen.findByText("This archived version cannot be imported again. Update version in codeagent-extension.json and import again.")).toBeInTheDocument();
  });

  it("previews a ZIP, lets the user edit the Squad base name, then imports fixed bindings", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    renderPage();
    const bytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04]);
    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(bytes)] },
    });

    expect(await screen.findByRole("button", { name: "确认导入" })).toBeInTheDocument();
    fireEvent.change(screen.getByLabelText("小队名称"), { target: { value: "delegation" } });
    fireEvent.click(screen.getByRole("button", { name: "确认导入" }));

    await waitFor(() => expect(apiClient.importPlatformExtension).toHaveBeenCalledWith(bytes, {
      squad_base_name: "delegation",
      agent_runtime_ids: { lead: runtime.id },
    }));
  });

  it("keeps an unconfirmed same-version preview out of import history", async () => {
    vi.mocked(apiClient.previewPlatformExtension).mockResolvedValue({
      ...preview,
      release: { ...preview.release, version: "1.0.0" },
    });
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(new Uint8Array([0x50, 0x4b, 0x03, 0x04]))] },
    });

    expect(await screen.findByRole("button", { name: "确认导入" })).toBeInTheDocument();
    expect(screen.getAllByTestId("extension-history-group-delegate")).toHaveLength(1);
  });

  it("expands version history and marks a same-version preview as pending", async () => {
    vi.mocked(apiClient.previewPlatformExtension).mockResolvedValue({
      ...preview,
      release: { ...preview.release, version: "1.0.0" },
    });
    renderPage();

    const historyToggle = await screen.findByRole("button", { name: "delegate" });
    fireEvent.click(historyToggle);
    expect(historyToggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByRole("button", { name: "v1.0.0" })).not.toBeInTheDocument();

    fireEvent.click(historyToggle);
    expect(historyToggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByRole("button", { name: "v1.0.0" })).toHaveTextContent("当前");

    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(new Uint8Array([0x50, 0x4b, 0x03, 0x04]))] },
    });

    expect(await screen.findByRole("button", { name: "确认导入" })).toBeInTheDocument();
    expect(screen.getAllByTestId("extension-history-group-delegate")).toHaveLength(1);
    expect(screen.getByRole("button", { name: "v1.0.0" })).toHaveTextContent("待确认");
  });

  it("visibly labels the editable Squad name in the instruction mapping", async () => {
    renderPage();

    expect(await screen.findByText("小队名称")).toBeInTheDocument();
    expect(screen.getByLabelText("小队名称")).toBeInTheDocument();
  });

  it("right-aligns editable mapping controls", async () => {
    renderPage();

    const controls = await screen.findAllByTestId("mapping-configuration");
    expect(controls).toHaveLength(2);
    controls.forEach((control) => expect(control).toHaveClass("ml-auto"));
  });

  it("uses a right-shifted shared source column so transfer arrows align vertically", async () => {
    renderPage();

    const sources = await screen.findAllByTestId("mapping-source");
    const arrows = screen.getAllByTestId("mapping-transfer-arrow");
    expect(sources).not.toHaveLength(0);
    expect(arrows).toHaveLength(sources.length);
    sources.forEach((source) => expect(source).toHaveClass("w-[340px]"));
  });

  it("adds a clear gap between the transfer arrow and mapped resource", async () => {
    renderPage();

    const targets = await screen.findAllByTestId("mapping-target");
    expect(targets).not.toHaveLength(0);
    targets.forEach((target) => expect(target).toHaveClass("ml-6"));
  });

  it("keeps the imported atomic mappings visible after confirmation", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    renderPage();
    const bytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04]);
    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(bytes)] },
    });

    fireEvent.click(await screen.findByRole("button", { name: "确认导入" }));
    await waitFor(() => expect(apiClient.importPlatformExtension).toHaveBeenCalledOnce());

    expect((await screen.findAllByText("已导入")).length).toBeGreaterThan(0);
    expect(screen.getByText("delegate-e2e")).toBeInTheDocument();
  });

  it("keeps a previously imported Flow Command mapped to Squad Instructions", async () => {
    vi.mocked(apiClient.getPlatformExtension).mockResolvedValue({
      ...detail,
      manifest: {
        ...detail.manifest,
        flow_commands: [{ name: "delegate.flow" }],
      },
    });
    renderPage();

    expect(await screen.findByText("SQUAD INSTRUCTIONS")).toBeInTheDocument();
    expect(screen.getByText("delegate.flow")).toBeInTheDocument();
  });

  it("turns imported mapping checks green after confirmation", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    renderPage();
    const bytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04]);
    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(bytes)] },
    });

    fireEvent.click(await screen.findByRole("button", { name: "确认导入" }));
    await waitFor(() => expect(apiClient.importPlatformExtension).toHaveBeenCalledOnce());

    const initial = await screen.findAllByTestId("mapping-progress-indicator");
    expect(initial).not.toHaveLength(0);
    expect(initial.every((indicator) => indicator.dataset.state === "pending")).toBe(true);

    await waitFor(() => {
      const indicators = screen.getAllByTestId("mapping-progress-indicator");
      expect(indicators.some((indicator) => indicator.dataset.state === "confirmed")).toBe(true);
    });
  });

  it("restores the saved imported mapping while keeping another version pending", async () => {
    vi.spyOn(apiClient, "updatePlatformExtension").mockResolvedValue({
      ...mapping,
      squad: { ...mapping.squad, name: "delegated · v1.0.0" },
    });
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(new Uint8Array([0x50, 0x4b, 0x03, 0x04]))] },
    });
    expect(await screen.findByRole("button", { name: "确认导入" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "v1.0.0" }));
    fireEvent.change(await screen.findByLabelText("小队名称"), { target: { value: "delegated" } });
    fireEvent.click(screen.getByRole("button", { name: "保存更改" }));

    await waitFor(() => expect(apiClient.updatePlatformExtension).toHaveBeenCalledOnce());
    expect(await screen.findByText("SQUAD INSTRUCTIONS")).toBeInTheDocument();
    expect(screen.getByText("delegate-e2e")).toBeInTheDocument();
    expect(screen.getAllByTestId("mapping-progress-indicator").every((indicator) => indicator.dataset.state === "confirmed")).toBe(true);

    const pendingVersion = screen.getByRole("button", { name: "v1.1.0" });
    expect(pendingVersion).toHaveTextContent("待确认");
    fireEvent.click(pendingVersion);
    expect(await screen.findByRole("button", { name: "确认导入" })).toBeInTheDocument();
  });

  it("allows importing an Extension with unbound internal Agents when no runtime is available", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    vi.mocked(apiClient.previewPlatformExtension).mockResolvedValue({
      ...preview,
      agents: [{ source_key: "lead", name: "Delegate Lead", leader: true, runtime_id: "" }],
      runtimes: [],
    });
    renderPage();
    const bytes = new Uint8Array([0x50, 0x4b, 0x03, 0x04]);
    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(bytes)] },
    });

    expect(await screen.findByText("暂无可用运行时")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "确认导入" }));
    await waitFor(() => expect(apiClient.importPlatformExtension).toHaveBeenCalledWith(bytes, {
      squad_base_name: "delegate",
      agent_runtime_ids: { lead: "" },
    }));
  });

  it("shows concise internal Agent names without the template label", async () => {
    const lead = detail.agents[0]!;
    vi.mocked(apiClient.getPlatformExtension).mockResolvedValue({
      ...detail,
      agents: [{
        ...lead,
        name: "Runtime Pool Demo v1.0.0 / Pool Coordinator",
      }],
    });
    renderPage();

    expect(await screen.findByTestId("extension-atomic-mapping")).toBeInTheDocument();
    expect(screen.getAllByText("Pool Coordinator")).toHaveLength(2);
    expect(screen.queryByText("Squad Instructions · 此版本模板")).not.toBeInTheDocument();
  });

  it("groups release versions and saves the editable Squad and internal Runtime mapping", async () => {
    const alternateRuntime = {
      id: "66666666-6666-4666-8666-666666666666",
      provider: "platform-agent-cli" as const,
      name: "My second Platform Agent CLI",
    };
    const earlier: PlatformExtensionMapping = {
      ...mapping,
      release: { ...mapping.release, id: "77777777-7777-4777-8777-777777777777", version: "1.0.0" },
      squad: { ...mapping.squad, name: "delegate · v1.0.0" },
    };
    const current: PlatformExtensionMapping = {
      ...mapping,
      release: { ...mapping.release, id: "88888888-8888-4888-8888-888888888888", version: "2.0.0" },
      squad: { ...mapping.squad, name: "delegate · v2.0.0" },
    };
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([current, earlier]);
    vi.mocked(apiClient.getPlatformExtension).mockResolvedValue({
      ...current,
      available_runtimes: [runtime, alternateRuntime],
      manifest: detail.manifest,
    });
    vi.spyOn(apiClient, "updatePlatformExtension").mockResolvedValue({
      ...current,
      squad: { ...current.squad, name: "research-delegate · v2.0.0" },
      agents: [{
        source_key: "lead",
        id: "44444444-4444-4444-8444-444444444444",
        name: "Delegate Lead",
        leader: true,
        runtime: alternateRuntime,
      }],
    });

    renderPage();

    expect(await screen.findByTestId("extension-history-group-delegate")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "v2.0.0" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "v1.0.0" })).toBeInTheDocument();
    expect(await screen.findByLabelText("小队名称")).toHaveValue("delegate");

    fireEvent.change(screen.getByLabelText("小队名称"), { target: { value: "research-delegate" } });
    fireEvent.change(screen.getByLabelText("Delegate Lead runtime"), { target: { value: alternateRuntime.id } });
    expect(screen.getByRole("button", { name: "保存更改" })).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "保存更改" }));

    await waitFor(() => expect(apiClient.updatePlatformExtension).toHaveBeenCalledWith(current.release.id, {
      squad_base_name: "research-delegate",
      agent_runtime_ids: { lead: alternateRuntime.id },
    }));
  });

  it("rejects a non-ZIP package before previewing", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    renderPage();
    fireEvent.change(screen.getByLabelText("Extension ZIP package"), {
      target: { files: [fileWithBytes(new Uint8Array([1]), "extension.json")] },
    });
    expect(await screen.findByText("Choose a .zip package.")).toBeInTheDocument();
    expect(apiClient.previewPlatformExtension).not.toHaveBeenCalled();
  });
});
