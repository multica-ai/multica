import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiClient, ApiError, setApiInstance } from "@multica/core/api";
import {
  I18nProvider,
  type I18nProviderProps,
} from "@multica/core/i18n/react";
import {
  PLATFORM_EXTENSION_MAX_IMPORT_BYTES,
  type PlatformExtensionDetail,
  type PlatformExtensionImportResult,
  type PlatformExtensionMapping,
} from "@multica/core/extensions";
import zhHansExtensions from "../locales/zh-Hans/extensions.json";
import { ExtensionsPage } from "./extensions-page";

const navigation = vi.hoisted(() => ({ push: vi.fn() }));

vi.mock("../navigation", () => ({
  AppLink: ({ children, href, ...props }: React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => navigation,
}));

vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    runtimeDetail: (id: string) => `/acme/runtimes/${id}`,
    squadDetail: (id: string) => `/acme/squads/${id}`,
    agentDetail: (id: string) => `/acme/agents/${id}`,
    skillDetail: (id: string) => `/acme/skills/${id}`,
  }),
}));

const resources = {
  en: {
    extensions: {
      page: {
        title: "Extensions",
        description: "Import and inspect platform extension releases.",
        import: "Import extension",
      },
      states: {
        loading: "Loading extensions",
        empty_title: "No extensions yet",
        empty_description: "Import a JSON extension document to get started.",
        list_error_title: "Extensions could not be loaded",
        list_error_description: "Try again in a moment.",
        detail_loading: "Loading release details",
        detail_error_title: "Release details could not be loaded",
        detail_error_description: "Choose the release again or try later.",
      },
      import: {
        file_label: "Extension JSON file",
        pending: "Importing extension",
        success: "Extension imported successfully",
        idempotent: "This release was already imported; existing resources were reused.",
        invalid_file: "Choose a .json file.",
        invalid_json: "The selected file is not valid JSON.",
        invalid_utf8: "The selected file must use valid UTF-8 encoding.",
        too_large: "The selected file exceeds the 5 MiB limit.",
        generic_error: "The extension could not be imported.",
        runtime_unavailable_title: "Platform Agent CLI runtime unavailable",
        runtime_unavailable_description:
          "Start or reconnect Platform Agent CLI for this workspace, then import the extension again.",
      },
      detail: {
        release: "Release",
        runtime: "Runtime",
        squad: "Squad",
        agents: "Agents",
        skills: "Skills",
        version: "Version {{version}}",
        digest: "Digest {{digest}}",
        leader: "Leader",
        no_agents: "No agents",
        no_skills: "No skills",
        platform_runtime: "Platform Agent CLI",
      },
    },
  },
};

const mapping: PlatformExtensionMapping = {
  release: {
    id: "release-1",
    extension_key: "acme.deploy",
    version: "1.2.3",
    digest: "sha256:abc",
  },
  runtime: {
    id: "runtime-1",
    provider: "platform-agent-cli",
    name: "internal runtime name",
  },
  squad: { id: "squad-1", name: "Deploy Squad" },
  agents: [
    {
      source_key: "reviewer",
      id: "agent-1",
      name: "Release Reviewer",
      leader: true,
    },
  ],
  skills: [
    {
      source_key: "deploy",
      id: "skill-1",
      name: "Deploy Service",
    },
  ],
};

const detail: PlatformExtensionDetail = { ...mapping, manifest: { name: "Deploy" } };
const importResult: PlatformExtensionImportResult = { ...mapping, idempotent: false };
let apiClient: ApiClient;

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function renderPage({
  locale = "en",
  i18nResources = resources,
}: {
  locale?: I18nProviderProps["locale"];
  i18nResources?: I18nProviderProps["resources"];
} = {}) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider locale={locale} resources={i18nResources}>
        <ExtensionsPage wsId="ws-1" />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

function fileWithBytes(bytes: Uint8Array, name = "extension.json") {
  const file = new File([Uint8Array.from(bytes).buffer], name, {
    type: "application/json",
  });
  Object.defineProperty(file, "arrayBuffer", {
    value: vi.fn().mockResolvedValue(
      bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
    ),
  });
  return file;
}

function jsonFile(value: unknown) {
  return fileWithBytes(new TextEncoder().encode(JSON.stringify(value)));
}

beforeEach(() => {
  navigation.push.mockReset();
  apiClient = new ApiClient("https://api.example.test");
  setApiInstance(apiClient);
  vi.spyOn(apiClient, "listPlatformExtensions").mockResolvedValue([mapping]);
  vi.spyOn(apiClient, "getPlatformExtension").mockResolvedValue(detail);
  vi.spyOn(apiClient, "importPlatformExtension").mockResolvedValue(importResult);
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("ExtensionsPage", () => {
  it("shows list loading and then release details with resource links", async () => {
    const pending = deferred<PlatformExtensionMapping[]>();
    vi.mocked(apiClient.listPlatformExtensions).mockReturnValue(pending.promise);

    renderPage();
    expect(screen.getByText("Loading extensions")).toBeInTheDocument();

    pending.resolve([mapping]);

    expect(await screen.findByText("acme.deploy")).toBeInTheDocument();
    expect(await screen.findByText("Version 1.2.3")).toBeInTheDocument();
    expect(screen.getByText("Digest sha256:abc")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Platform Agent CLI" })).toHaveAttribute(
      "href",
      "/acme/runtimes/runtime-1",
    );
    expect(screen.getByRole("link", { name: "Deploy Squad" })).toHaveAttribute(
      "href",
      "/acme/squads/squad-1",
    );
    expect(screen.getByRole("link", { name: /Release Reviewer/ })).toHaveAttribute(
      "href",
      "/acme/agents/agent-1",
    );
    expect(screen.getByRole("link", { name: "Deploy Service" })).toHaveAttribute(
      "href",
      "/acme/skills/skill-1",
    );
  });

  it("keeps long release and resource names readable without overflowing cards", async () => {
    const longVersion = `v${"v".repeat(299)}`;
    const longSquad = `q${"q".repeat(299)}`;
    const longAgent = `a${"a".repeat(299)}`;
    const longSkill = `k${"k".repeat(299)}`;
    const longMapping: PlatformExtensionMapping = {
      ...mapping,
      release: { ...mapping.release, version: longVersion },
      squad: { ...mapping.squad, name: longSquad },
      agents: [{ ...mapping.agents[0]!, name: longAgent }],
      skills: [{ ...mapping.skills[0]!, name: longSkill }],
    };
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([longMapping]);
    vi.mocked(apiClient.getPlatformExtension).mockResolvedValue({
      ...longMapping,
      manifest: { name: "Long names" },
    });

    renderPage();

    await waitFor(() => {
      expect(screen.getAllByText(`Version ${longVersion}`)).toHaveLength(2);
    });
    const versionLabels = screen.getAllByText(`Version ${longVersion}`);
    for (const label of versionLabels) {
      expect(label).toHaveClass("min-w-0", "break-words", "[overflow-wrap:anywhere]");
    }
    expect(versionLabels[0]?.closest("button")).toHaveClass(
      "min-w-0",
      "max-w-full",
    );

    const squadLink = screen.getByRole("link", { name: longSquad });
    expect(squadLink).toHaveClass(
      "min-w-0",
      "max-w-full",
      "break-words",
      "[overflow-wrap:anywhere]",
    );
    expect(squadLink.closest('[data-slot="card"]')).toHaveClass(
      "min-w-0",
      "max-w-full",
    );

    const agentLink = screen.getByRole("link", {
      name: `${longAgent} — Leader`,
    });
    expect(agentLink).toHaveClass(
      "min-w-0",
      "max-w-full",
      "break-words",
      "[overflow-wrap:anywhere]",
    );

    const skillLink = screen.getByRole("link", { name: longSkill });
    expect(skillLink).toHaveAttribute("title", longSkill);
    expect(skillLink).toHaveClass("min-w-0", "max-w-full", "truncate");
    expect(skillLink.closest('[data-slot="card"]')).toHaveClass(
      "min-w-0",
      "max-w-full",
    );
  });

  it("uses the product term skill in zh-Hans extension details", async () => {
    renderPage({
      locale: "zh-Hans",
      i18nResources: { "zh-Hans": { extensions: zhHansExtensions } },
    });

    expect(await screen.findByText("Skills")).toBeInTheDocument();
  });

  it("shows the empty state", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText("No extensions yet")).toBeInTheDocument();
  });

  it("shows list and detail failures", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockRejectedValueOnce(new Error("offline"));
    const first = renderPage();
    expect(
      await screen.findByText("Extensions could not be loaded"),
    ).toBeInTheDocument();
    first.unmount();

    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([mapping]);
    vi.mocked(apiClient.getPlatformExtension).mockRejectedValue(new Error("broken"));
    renderPage();
    expect(
      await screen.findByText("Release details could not be loaded"),
    ).toBeInTheDocument();
  });

  it("reads a JSON file as raw bytes and displays the imported mapping", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    const file = jsonFile({ extension_key: "acme.deploy" });
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension JSON file"), {
      target: { files: [file] },
    });

    await waitFor(() => expect(apiClient.importPlatformExtension).toHaveBeenCalledTimes(1));
    const bytes = vi.mocked(apiClient.importPlatformExtension).mock.calls[0]?.[0];
    expect(bytes).toBeInstanceOf(Uint8Array);
    expect(new TextDecoder().decode(bytes as Uint8Array)).toBe(
      JSON.stringify({ extension_key: "acme.deploy" }),
    );
    expect(
      await screen.findByText("Extension imported successfully"),
    ).toBeInTheDocument();
    expect(await screen.findByText("Deploy Squad")).toBeInTheDocument();
  });

  it("explains when an import reuses an existing release", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    vi.mocked(apiClient.importPlatformExtension).mockResolvedValue({
      ...importResult,
      idempotent: true,
    });
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension JSON file"), {
      target: { files: [jsonFile({ extension_key: "acme.deploy" })] },
    });

    expect(
      await screen.findByText(
        "This release was already imported; existing resources were reused.",
      ),
    ).toBeInTheDocument();
  });

  it("rejects the wrong extension and malformed JSON before import", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    renderPage();
    const input = screen.getByLabelText("Extension JSON file");

    fireEvent.change(input, {
      target: {
        files: [
          fileWithBytes(new TextEncoder().encode(JSON.stringify({})), "extension.txt"),
        ],
      },
    });
    expect(await screen.findByText("Choose a .json file.")).toBeInTheDocument();

    fireEvent.change(input, {
      target: { files: [fileWithBytes(new TextEncoder().encode("{broken"))] },
    });
    expect(
      await screen.findByText("The selected file is not valid JSON."),
    ).toBeInTheDocument();
    expect(apiClient.importPlatformExtension).not.toHaveBeenCalled();
  });

  it("rejects oversized and invalid UTF-8 bytes before import", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    renderPage();
    const input = screen.getByLabelText("Extension JSON file");

    fireEvent.change(input, {
      target: {
        files: [
          fileWithBytes(new Uint8Array(PLATFORM_EXTENSION_MAX_IMPORT_BYTES + 1)),
        ],
      },
    });
    expect(
      await screen.findByText("The selected file exceeds the 5 MiB limit."),
    ).toBeInTheDocument();

    fireEvent.change(input, {
      target: { files: [fileWithBytes(new Uint8Array([0xc3, 0x28]))] },
    });
    expect(
      await screen.findByText(
        "The selected file must use valid UTF-8 encoding.",
      ),
    ).toBeInTheDocument();
    expect(apiClient.importPlatformExtension).not.toHaveBeenCalled();
  });

  it("accepts a valid JSON document at the exact 5 MiB boundary", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    const bytes = new Uint8Array(PLATFORM_EXTENSION_MAX_IMPORT_BYTES);
    bytes.fill(0x20);
    bytes[0] = 0x7b;
    bytes[bytes.length - 1] = 0x7d;
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension JSON file"), {
      target: { files: [fileWithBytes(bytes)] },
    });

    await waitFor(() =>
      expect(apiClient.importPlatformExtension).toHaveBeenCalledTimes(1),
    );
    expect(
      (vi.mocked(apiClient.importPlatformExtension).mock.calls[0]?.[0] as Uint8Array)
        .byteLength,
    ).toBe(PLATFORM_EXTENSION_MAX_IMPORT_BYTES);
  });

  it("shows pending state and a generic import error", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    const pending = deferred<PlatformExtensionImportResult | null>();
    vi.mocked(apiClient.importPlatformExtension).mockReturnValue(pending.promise);
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension JSON file"), {
      target: { files: [jsonFile({ extension_key: "acme.deploy" })] },
    });
    expect(await screen.findByText("Importing extension")).toBeInTheDocument();

    pending.reject(new Error("network"));
    expect(
      await screen.findByText("The extension could not be imported."),
    ).toBeInTheDocument();
  });

  it("shows actionable recovery for PLATFORM_RUNTIME_UNAVAILABLE", async () => {
    vi.mocked(apiClient.listPlatformExtensions).mockResolvedValue([]);
    vi.mocked(apiClient.importPlatformExtension).mockRejectedValue(
      new ApiError("conflict", 409, "Conflict", {
        code: "PLATFORM_RUNTIME_UNAVAILABLE",
      }),
    );
    renderPage();

    fireEvent.change(screen.getByLabelText("Extension JSON file"), {
      target: { files: [jsonFile({ extension_key: "acme.deploy" })] },
    });

    expect(
      await screen.findByText("Platform Agent CLI runtime unavailable"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Start or reconnect Platform Agent CLI for this workspace, then import the extension again.",
      ),
    ).toBeInTheDocument();
  });
});
