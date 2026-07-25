import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { Command, CommandInput } from "@multica/ui/components/ui/command";

// Contract for the shared CommandInput: Home/End must stop propagating to
// cmdk's root keydown handler (which otherwise hijacks them to jump the result
// list to first/last), while every key — including Home/End — is still handed
// to a caller-provided onKeyDown. cmdk lives in @multica/ui, which has no test
// runner, so the contract is pinned here alongside the SearchCommand
// regression that depends on it.
describe("CommandInput", () => {
  const renderInput = () => {
    // An ancestor listener outside <Command> is the propagation probe: it only
    // fires if the keydown bubbled past the input (i.e. past cmdk's root).
    const ancestorKeyDown = vi.fn();
    const callerKeyDown = vi.fn();
    render(
      <div onKeyDown={ancestorKeyDown}>
        <Command>
          <CommandInput placeholder="search" onKeyDown={callerKeyDown} />
        </Command>
      </div>,
    );
    return { ancestorKeyDown, callerKeyDown };
  };

  it.each(["{Home}", "{End}"])(
    "stops %s from bubbling past the input while still calling the caller onKeyDown",
    async (key) => {
      const user = userEvent.setup();
      const { ancestorKeyDown, callerKeyDown } = renderInput();

      await user.click(screen.getByPlaceholderText("search"));
      await user.keyboard(key);

      expect(callerKeyDown).toHaveBeenCalledTimes(1);
      expect(ancestorKeyDown).not.toHaveBeenCalled();
    },
  );

  it("lets other keys bubble so cmdk list navigation keeps working", async () => {
    const user = userEvent.setup();
    const { ancestorKeyDown, callerKeyDown } = renderInput();

    await user.click(screen.getByPlaceholderText("search"));
    await user.keyboard("{ArrowDown}");

    // Not Home/End, so the interception must not apply: the event reaches the
    // ancestor (and thus cmdk's root), and the caller callback still runs.
    expect(callerKeyDown).toHaveBeenCalledTimes(1);
    expect(ancestorKeyDown).toHaveBeenCalledTimes(1);
  });
});
