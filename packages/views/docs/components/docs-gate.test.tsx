import { act, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { configStore } from "@multica/core/config";
import { DOCS_V1_FLAG } from "@multica/core/feature-flags";
import { renderWithI18n } from "../../test/i18n";
import { DocsGate } from "./docs-gate";

describe("DocsGate", () => {
  beforeEach(() => {
    configStore.getState().setFeatureFlags({});
  });

  it("shows an empty state instead of the page while docs_v1 is off", () => {
    renderWithI18n(<DocsGate><p>doc page</p></DocsGate>);
    expect(screen.queryByText("doc page")).toBeNull();
    expect(screen.getByText("Docs isn't available here")).toBeTruthy();
  });

  it("renders the page once the server config turns docs_v1 on", () => {
    renderWithI18n(<DocsGate><p>doc page</p></DocsGate>);
    act(() => configStore.getState().setFeatureFlags({ [DOCS_V1_FLAG]: true }));
    expect(screen.getByText("doc page")).toBeTruthy();
  });
});
