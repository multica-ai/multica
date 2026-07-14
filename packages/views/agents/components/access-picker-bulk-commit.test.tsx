// @vitest-environment jsdom

import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import { AccessPicker } from "./inspector/access-picker";

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
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AccessPicker
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
    </I18nProvider>
  );
}

describe("AccessPicker bulk mode (hideFooter)", () => {
  it("isReady starts false (private is the persisted default)", () => {
    const readyFn = vi.fn();
    render(<Harness onReadyChange={readyFn} />);
    expect(readyFn).toHaveBeenCalledWith(false);
  });

  it("clicking Workspace radio fires onReadyChange(true)", () => {
    const readyFn = vi.fn();
    render(<Harness onReadyChange={readyFn} />);
    const workspaceRadio = screen.getByRole("radio", {
      name: /Entire workspace/,
    });
    fireEvent.click(workspaceRadio);
    expect(readyFn).toHaveBeenCalledWith(true);
  });

  it("clicking Specific people without selecting a member keeps onReadyChange(false)", () => {
    const readyFn = vi.fn();
    render(<Harness onReadyChange={readyFn} />);
    const membersRadio = screen.getByRole("radio", {
      name: /Specific people/,
    });
    fireEvent.click(membersRadio);
    expect(readyFn).toHaveBeenCalledWith(false);
  });
});
