import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useEnvironmentWindowTitle } from "./use-environment-window-title";

describe("useEnvironmentWindowTitle", () => {
  beforeEach(() => {
    document.title = "Issues";
  });

  afterEach(() => {
    // Ensure instance override is cleared between tests.
    // eslint-disable-next-line @typescript-eslint/no-dynamic-delete
    delete (document as { title?: string }).title;
    document.title = "";
  });

  it("does nothing when there is no environment hint (Cloud)", () => {
    renderHook(() => useEnvironmentWindowTitle(null));
    expect(document.title).toBe("Issues");
  });

  it("suffixes the current document title with the server name", () => {
    renderHook(() =>
      useEnvironmentWindowTitle({
        name: "Personal",
        apiUrl: "http://127.0.0.1:28443",
      }),
    );
    expect(document.title).toBe("Issues · Personal");
  });

  it("re-applies the suffix when page code changes the title", () => {
    renderHook(() =>
      useEnvironmentWindowTitle({
        name: "Personal",
        apiUrl: "http://127.0.0.1:28443",
      }),
    );
    expect(document.title).toBe("Issues · Personal");

    // Simulate TitleSync / useDocumentTitle writing a bare page title.
    document.title = "Agents";
    expect(document.title).toBe("Agents · Personal");
  });

  it("does not double-append on repeated applies", () => {
    const { rerender } = renderHook(
      ({ hint }) => useEnvironmentWindowTitle(hint),
      {
        initialProps: {
          hint: {
            name: "Personal",
            apiUrl: "http://127.0.0.1:28443",
          },
        },
      },
    );
    document.title = "Issues · Personal";
    rerender({
      hint: {
        name: "Personal",
        apiUrl: "http://127.0.0.1:28443",
      },
    });
    expect(document.title).toBe("Issues · Personal");
  });
});
