import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import { ActivatePage } from "./activate-page";
import en from "../locales/en/auth.json";
import es from "../locales/es/auth.json";
import fr from "../locales/fr/auth.json";
import ja from "../locales/ja/auth.json";
import ko from "../locales/ko/auth.json";
import ru from "../locales/ru/auth.json";
import zhHans from "../locales/zh-Hans/auth.json";
import zhHant from "../locales/zh-Hant/auth.json";

const apiMock = vi.hoisted(() => ({
  inspectDeviceAuthorization: vi.fn(),
  approveDeviceAuthorization: vi.fn(),
}));

vi.mock("@multica/core/api", async () => ({
  ...(await vi.importActual("@multica/core/api")),
  api: apiMock,
}));

vi.mock("@multica/core/auth", () => {
  const state = { user: { email: "approver@example.com" } };
  return {
    useAuthStore: Object.assign(
      (selector: (s: typeof state) => unknown) => selector(state),
      { getState: () => state },
    ),
  };
});

const locales = { en, es, fr, ja, ko, ru, "zh-Hans": zhHans, "zh-Hant": zhHant };
type Locale = keyof typeof locales;

function mount(locale: Locale = "en") {
  render(
    <I18nProvider locale={locale} resources={{ [locale]: { auth: locales[locale] } }}>
      <ActivatePage />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  apiMock.inspectDeviceAuthorization.mockResolvedValue({ client_name: "build-runner-01" });
  apiMock.approveDeviceAuthorization.mockResolvedValue(undefined);
});

describe("ActivatePage device confirmation", () => {
  it.each(Object.keys(locales) as Locale[])(
    "shows the requesting machine and requires a separate confirmation in %s",
    async (locale) => {
      mount(locale);
      const text = locales[locale].activate;
      fireEvent.change(screen.getByRole("textbox", { name: text.code_label }), {
        target: { value: "abcd2345" },
      });
      expect(apiMock.approveDeviceAuthorization).not.toHaveBeenCalled();

      fireEvent.click(screen.getByRole("button", { name: text.continue }));
      await screen.findByText("build-runner-01");
      expect(apiMock.inspectDeviceAuthorization).toHaveBeenCalledWith("ABCD2345");
      expect(apiMock.approveDeviceAuthorization).not.toHaveBeenCalled();

      fireEvent.click(screen.getByRole("button", { name: text.confirm }));
      await screen.findByText(text.approved_title);
      expect(apiMock.approveDeviceAuthorization).toHaveBeenCalledWith("ABCD2345");
    },
  );

  it("does not report success when approval fails", async () => {
    apiMock.approveDeviceAuthorization.mockRejectedValue(new Error("offline"));
    mount();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "ABCD2345" } });
    fireEvent.click(screen.getByRole("button", { name: en.activate.continue }));
    await screen.findByText("build-runner-01");

    fireEvent.click(screen.getByRole("button", { name: en.activate.confirm }));
    await screen.findByText(en.activate.errors.approve_failed);
    expect(screen.queryByText(en.activate.approved_title)).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: en.activate.back }));
    await waitFor(() => expect(screen.getByRole("textbox")).toHaveValue(""));
  });

  it("explains an invalid or expired code and returns to code entry", async () => {
    apiMock.inspectDeviceAuthorization.mockRejectedValue(
      new ApiError("invalid or expired code", 400, "Bad Request", {
        code: "invalid_device_code",
      }),
    );
    mount();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "ABCD2345" } });
    fireEvent.click(screen.getByRole("button", { name: en.activate.continue }));

    await screen.findByText(en.activate.errors.invalid_code);
    expect(screen.getByRole("textbox")).toHaveValue("");
    expect(apiMock.approveDeviceAuthorization).not.toHaveBeenCalled();
  });

  it("names an unknown machine when the CLI sent none", async () => {
    apiMock.inspectDeviceAuthorization.mockResolvedValue({ client_name: "" });
    mount();
    fireEvent.change(screen.getByRole("textbox"), { target: { value: "ABCD2345" } });
    fireEvent.click(screen.getByRole("button", { name: en.activate.continue }));
    await screen.findByText(en.activate.unknown_machine);
  });
});
