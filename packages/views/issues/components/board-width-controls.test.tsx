import { afterEach, describe, expect, it } from "vitest";
import { act, cleanup, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createStore } from "zustand/vanilla";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { viewStoreSlice, type IssueViewState } from "@multica/core/issues/stores/view-store";
import { renderWithI18n } from "../../test/i18n";
import { BoardWidthControls } from "./board-width-controls";

afterEach(cleanup);

function setup() {
  const store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  renderWithI18n(
    <ViewStoreProvider store={store}>
      <BoardWidthControls />
    </ViewStoreProvider>,
  );
  return store;
}

describe("board width controls", () => {
  it("labels the slider and updates its visible value with the keyboard", async () => {
    const user = userEvent.setup();
    const store = setup();
    const slider = await screen.findByRole("slider", { name: "Column width" });
    expect(screen.getByText("280 px")).toBeTruthy();
    act(() => slider.focus());
    await user.keyboard("{ArrowLeft}");
    expect(store.getState().boardColumnWidth).toBe(270);
    expect(screen.getByText("270 px")).toBeTruthy();
  });

  it("disables manual adjustment while fitting and restores it when fitting is turned off", async () => {
    const user = userEvent.setup();
    const store = setup();
    const fitButton = screen.getByRole("button", { name: "Fit to screen" });
    const slider = await screen.findByRole("slider", { name: "Column width" });
    act(() => slider.focus());
    await user.keyboard("{ArrowLeft}");
    await user.click(fitButton);
    expect(store.getState().boardFitToScreen).toBe(true);
    expect(fitButton).toHaveAttribute("aria-pressed", "true");
    expect(slider).toBeDisabled();
    expect(screen.getByText("270 px")).toBeTruthy();
    await user.click(fitButton);
    act(() => slider.focus());
    await user.keyboard("{ArrowRight}");
    expect(store.getState().boardFitToScreen).toBe(false);
    expect(store.getState().boardColumnWidth).toBe(280);
  });
});
