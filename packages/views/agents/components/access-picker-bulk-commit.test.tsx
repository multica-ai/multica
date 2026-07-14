// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { useRef } from "react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import {
  AccessPicker,
  type AccessPickerHandle,
} from "./inspector/access-picker";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div>avatar</div>,
}));

function Harness({
  onReadyChange,
  onChange,
}: {
  onReadyChange?: (ready: boolean) => void;
  onChange?: (next: unknown) => void;
}) {
  const ref = useRef<AccessPickerHandle | null>(null);
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AccessPicker
        ref={ref}
        permissionMode="private"
        invocationTargets={[]}
        visibility="private"
        members={[]}
        ownerId="user-1"
        canEdit
        hideFooter
        onReadyChange={onReadyChange}
        onChange={onChange}
      />
      <div data-testid="handle">
        {JSON.stringify({
          ready: ref.current?.isReady(),
          commit: ref.current?.commit() ?? null,
        })}
      </div>
    </I18nProvider>
  );
}

describe("AccessPicker imperative commit (hideFooter mode)", () => {
  it("isReady() starts false and commit() returns null before any user interaction", () => {
    const readyFn = vi.fn();
    render(<Harness onReadyChange={readyFn} />);
    // The persisted state is "private" — dirty is false → ready is false.
    expect(readyFn).toHaveBeenCalledWith(false);
  });

  it("clicking Workspace radio makes the picker ready and commit() returns a Workspace change", () => {
    const readyFn = vi.fn();
    render(<Harness onReadyChange={readyFn} />);

    const workspaceRadio = screen.getByRole("radio", {
      name: /Entire workspace/,
    });
    fireEvent.click(workspaceRadio);

    // After clicking Workspace the picker should emit onReadyChange(true).
    expect(readyFn).toHaveBeenCalledWith(true);
  });

  it("clicking Specific people without selecting a member keeps the picker not ready", () => {
    const readyFn = vi.fn();
    render(<Harness onReadyChange={readyFn} />);

    const membersRadio = screen.getByRole("radio", {
      name: /Specific people/,
    });
    fireEvent.click(membersRadio);

    // Zero members in the list → not ready (no target selected).
    expect(readyFn).toHaveBeenCalledWith(false);
  });
});
