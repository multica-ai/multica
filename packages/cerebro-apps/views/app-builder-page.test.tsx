// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const push = vi.fn();
const createApp = vi.fn();

vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => true }));
vi.mock("@multica/core/paths", () => ({ useCurrentWorkspace: () => ({ id: "ws-1", slug: "firtal" }) }));
vi.mock("@multica/views/navigation", () => ({ useNavigation: () => ({ push }) }));
vi.mock("../core/api", () => ({ createApp: (input: unknown) => createApp(input) }));

import { AppBuilderPage } from "./app-builder-page";

describe("AppBuilderPage", () => {
  beforeEach(() => {
    push.mockReset();
    createApp.mockReset();
    createApp.mockResolvedValue({ id: "app-1", name: "Returns helper" });
  });

  it("creates an app and opens its detail page", async () => {
    render(<AppBuilderPage />);
    await userEvent.type(screen.getByLabelText("Name"), "Returns helper");
    await userEvent.type(screen.getByLabelText("Description"), "Look up and update returns");
    await userEvent.click(screen.getByRole("button", { name: "Create app" }));

    expect(createApp).toHaveBeenCalledWith(expect.objectContaining({
      name: "Returns helper",
      slug: "returns-helper",
      description: "Look up and update returns",
    }));
    expect(push).toHaveBeenCalledWith("/firtal/apps/app-1");
  });
});
