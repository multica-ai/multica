// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentRuntime } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enRuntimes from "../../locales/en/runtimes.json";
import { BuiltInRuntimeOffer } from "./built-in-runtime-offer";

const TEST_RESOURCES = { en: { common: enCommon, runtimes: enRuntimes } };

const validateMutateAsync = vi.hoisted(() => vi.fn());
const saveMutateAsync = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/runtimes/mutations", () => ({
  useValidateModelConnection: () => ({
    mutateAsync: validateMutateAsync,
    isPending: false,
  }),
  useUpdateRuntimeModelConnection: () => ({
    mutateAsync: saveMutateAsync,
    isPending: false,
  }),
}));

const runtime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "workspace-1",
  daemon_id: "daemon-1",
  name: "Pi",
  runtime_mode: "local",
  provider: "pi",
  launch_header: "",
  status: "online",
  device_info: "host",
  metadata: {},
  default_model_config: {},
  has_default_model_api_key: false,
  owner_id: "user-1",
  visibility: "private",
  profile_id: null,
  last_seen_at: null,
  created_at: "",
  updated_at: "",
} as AgentRuntime;

function renderOffer(props: Partial<Parameters<typeof BuiltInRuntimeOffer>[0]> = {}) {
  const onInstall = props.onInstall ?? vi.fn().mockResolvedValue({ success: true });
  const onConnected = props.onConnected ?? vi.fn();
  render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <BuiltInRuntimeOffer
        wsId="workspace-1"
        runtimes={[]}
        setup={null}
        onInstall={onInstall}
        onConnected={onConnected}
        {...props}
      />
    </I18nProvider>,
  );
  return { onInstall, onConnected };
}

beforeEach(() => {
  validateMutateAsync.mockReset();
  saveMutateAsync.mockReset();
});

describe("BuiltInRuntimeOffer", () => {
  it("never installs without an explicit click", async () => {
    const { onInstall } = renderOffer();
    // The whole point of moving the trigger out of daemon startup: rendering
    // the offer must not download anything.
    expect(onInstall).not.toHaveBeenCalled();

    await userEvent.click(
      screen.getByRole("button", { name: enRuntimes.built_in.offer_action }),
    );
    expect(onInstall).toHaveBeenCalledTimes(1);
  });

  it("shows why an install failed instead of a bare 'failed'", () => {
    renderOffer({
      setup: {
        provider: "pi",
        phase: "failed",
        startedAt: "t",
        error: "download failed: 403 Forbidden",
      },
    });

    expect(screen.getByRole("alert")).toHaveTextContent(
      "download failed: 403 Forbidden",
    );
    expect(
      screen.getByRole("button", { name: enRuntimes.built_in.retry_action }),
    ).toBeInTheDocument();
  });

  it("keeps waiting while the binary lands but the daemon has not registered it", () => {
    renderOffer({ setup: { provider: "pi", phase: "ready", startedAt: "t" } });

    expect(
      screen.getByText(enRuntimes.built_in.installing_registering),
    ).toBeInTheDocument();
  });

  it("asks for a key once the runtime registers with no model", () => {
    renderOffer({ runtimes: [runtime] });

    expect(
      screen.getByText(enRuntimes.built_in.connect.title),
    ).toBeInTheDocument();
    expect(screen.getByLabelText(/API key/i)).toBeInTheDocument();
  });

  it("verifies the key before saving, and saves only when it is accepted", async () => {
    validateMutateAsync.mockResolvedValue({ valid: true });
    saveMutateAsync.mockResolvedValue({});
    const { onConnected } = renderOffer({ runtimes: [runtime] });

    await userEvent.type(screen.getByLabelText(/API key/i), "sk-good-key");
    await userEvent.click(
      screen.getByRole("button", { name: enRuntimes.built_in.connect.submit }),
    );

    await waitFor(() => expect(saveMutateAsync).toHaveBeenCalledTimes(1));
    // Verification has to happen first — that is the whole point of the step.
    expect(validateMutateAsync).toHaveBeenCalledTimes(1);
    expect(validateMutateAsync.mock.invocationCallOrder[0]).toBeLessThan(
      saveMutateAsync.mock.invocationCallOrder[0]!,
    );
    expect(saveMutateAsync).toHaveBeenCalledWith(
      expect.objectContaining({
        runtimeId: "runtime-1",
        connection: expect.objectContaining({ api_key: "sk-good-key" }),
      }),
    );
    expect(onConnected).toHaveBeenCalled();
  });

  it("does not save a key the provider rejected", async () => {
    validateMutateAsync.mockResolvedValue({
      valid: false,
      outcome: "invalid_key",
      detail: "authentication failed",
    });
    const { onConnected } = renderOffer({ runtimes: [runtime] });

    await userEvent.type(screen.getByLabelText(/API key/i), "sk-bad-key");
    await userEvent.click(
      screen.getByRole("button", { name: enRuntimes.built_in.connect.submit }),
    );

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        enRuntimes.built_in.connect.error_invalid_key,
      ),
    );
    expect(saveMutateAsync).not.toHaveBeenCalled();
    expect(onConnected).not.toHaveBeenCalled();
  });

  it("explains an outcome a newer backend introduced rather than showing nothing", async () => {
    validateMutateAsync.mockResolvedValue({
      valid: false,
      outcome: "unknown",
      detail: "region blocked",
    });
    renderOffer({ runtimes: [runtime] });

    await userEvent.type(screen.getByLabelText(/API key/i), "sk-key");
    await userEvent.click(
      screen.getByRole("button", { name: enRuntimes.built_in.connect.submit }),
    );

    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(
        enRuntimes.built_in.connect.error_generic,
      ),
    );
  });

  it("links to the selected provider's key page", async () => {
    renderOffer({ runtimes: [runtime] });

    const link = screen.getByRole("link", { name: /Get an? .* API key/i });
    expect(link).toHaveAttribute("href", expect.stringContaining("https://"));
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("offers a way out for a user who has no API key yet", async () => {
    const onSkipKey = vi.fn();
    renderOffer({ runtimes: [runtime], onSkipKey });

    await userEvent.click(
      screen.getByRole("button", { name: enRuntimes.built_in.connect.no_key }),
    );
    expect(onSkipKey).toHaveBeenCalled();
  });

  it("reports the runtime as ready once it has a model", () => {
    renderOffer({
      runtimes: [
        {
          ...runtime,
          has_default_model_api_key: true,
          default_model_config: {
            provider: "deepseek",
            api: "openai-completions",
            base_url: "https://api.deepseek.com",
            model: "deepseek-v4-flash",
          },
        } as AgentRuntime,
      ],
    });

    expect(screen.getByText(enRuntimes.built_in.ready_title)).toBeInTheDocument();
  });
});
