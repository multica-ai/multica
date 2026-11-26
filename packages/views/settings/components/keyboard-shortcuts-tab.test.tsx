import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import {
  createShortcutChord,
  configureShortcutPlatform,
  configureShortcutRuntime,
  getShortcut,
  useShortcutStore,
} from "@multica/core/shortcuts";
import { renderWithI18n } from "../../test/i18n";
import { KeyboardShortcutsTab } from "./keyboard-shortcuts-tab";

describe("KeyboardShortcutsTab", () => {
  beforeEach(() => {
    configureShortcutPlatform("windows");
    useShortcutStore.getState().resetAll();
  });

  afterEach(() => {
    cleanup();
    configureShortcutPlatform(null);
    configureShortcutRuntime(null);
    useShortcutStore.getState().resetAll();
  });

  it("shows distinct left and right sidebar actions", () => {
    renderWithI18n(<KeyboardShortcutsTab />);

    expect(
      screen.getByRole("button", {
        name: "Change shortcut for Toggle left sidebar",
      }),
    ).toBeInTheDocument();
    const rightSidebarRecorder = screen.getByRole("button", {
      name: "Change shortcut for Toggle right sidebar",
    });
    expect(within(rightSidebarRecorder).getByTitle("Ctrl")).toHaveTextContent(
      "Ctrl",
    );
    expect(within(rightSidebarRecorder).getByTitle("/")).toHaveTextContent("/");
  });

  it("shows the fixed numbered tab shortcuts", () => {
    renderWithI18n(<KeyboardShortcutsTab />);

    expect(screen.getByText("Select tab 1–8")).toBeInTheDocument();
    expect(screen.getByText("Select last tab")).toBeInTheDocument();
    expect(
      screen.getByRole("img", { name: "Ctrl+1–8" }),
    ).toBeInTheDocument();
    expect(screen.getByRole("img", { name: "Ctrl+9" })).toBeInTheDocument();
  });

  it("records a shortcut and applies it immediately", () => {
    renderWithI18n(<KeyboardShortcutsTab />);
    const recorder = screen.getByRole("button", {
      name: "Change shortcut for Open search",
    });

    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, { key: "e", ctrlKey: true });

    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("E", { primary: true }),
    );
    expect(within(recorder).getByTitle("Ctrl")).toHaveTextContent("Ctrl");
    expect(within(recorder).getByTitle("E")).toHaveTextContent("E");
  });

  it("records logical punctuation from a physical number-row key", () => {
    configureShortcutRuntime("desktop");
    renderWithI18n(<KeyboardShortcutsTab />);
    const recorder = screen.getByRole("button", {
      name: "Change shortcut for Open search",
    });

    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, {
      key: "&",
      code: "Digit1",
      ctrlKey: true,
    });

    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("&", { primary: true }),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("records macOS Option chords by physical key, including dead keys", () => {
    configureShortcutPlatform("macos");
    renderWithI18n(<KeyboardShortcutsTab />);
    const recorder = screen.getByRole("button", {
      name: "Change shortcut for Open search",
    });

    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, {
      key: "Dead",
      code: "KeyE",
      metaKey: true,
      altKey: true,
    });

    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("E", { primary: true, alt: true }),
    );
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("reports a conflict with an Option chord saved under its glyph", () => {
    configureShortcutPlatform("macos");
    const legacy = createShortcutChord("©", { primary: true, alt: true });
    useShortcutStore.getState().setShortcut("openSearch", legacy);
    renderWithI18n(<KeyboardShortcutsTab />);
    const recorder = screen.getByRole("button", {
      name: "Change shortcut for Create issue",
    });

    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, {
      key: "©",
      code: "KeyG",
      metaKey: true,
      altKey: true,
    });

    expect(getShortcut("createIssue")).toEqual(createShortcutChord("C"));
    expect(getShortcut("openSearch")).toEqual(legacy);
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Already used by Open search.",
    );
  });

  it("explains a key it cannot record instead of ignoring it", () => {
    renderWithI18n(<KeyboardShortcutsTab />);
    const recorder = screen.getByRole("button", {
      name: "Change shortcut for Open search",
    });

    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, { key: "Control", ctrlKey: true });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();

    fireEvent.keyDown(recorder, { key: "Unidentified", ctrlKey: true });
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("K", { primary: true }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This key combination can't be recorded. Try another one.",
    );
  });

  it("only captures keys while the recorder is active", () => {
    renderWithI18n(<KeyboardShortcutsTab />);
    const recorder = screen.getByRole("button", {
      name: "Change shortcut for Open search",
    });

    recorder.focus();
    fireEvent.keyDown(recorder, { key: "e", ctrlKey: true });
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("K", { primary: true }),
    );

    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, { key: "e", ctrlKey: true });
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("E", { primary: true }),
    );

    // Focus remains on the button after a successful recording. Tab must move
    // focus normally instead of silently replacing the shortcut with Tab.
    fireEvent.keyDown(recorder, { key: "Tab" });
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("E", { primary: true }),
    );
  });

  it("rejects unsafe plain keys in editors while allowing Send = Enter", () => {
    renderWithI18n(<KeyboardShortcutsTab />);
    const searchRecorder = screen.getByRole("button", {
      name: "Change shortcut for Open search",
    });
    fireEvent.click(searchRecorder);
    fireEvent.keyDown(searchRecorder, { key: "j" });
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("K", { primary: true }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This key would interfere with typing or basic keyboard navigation.",
    );

    const sendRecorder = screen.getByRole("button", {
      name: "Change shortcut for Send",
    });
    fireEvent.click(sendRecorder);
    fireEvent.keyDown(sendRecorder, { key: "Enter" });
    expect(getShortcut("send")).toEqual(createShortcutChord("Enter"));
  });

  it("only allows Enter or Primary+Enter for Send", () => {
    renderWithI18n(<KeyboardShortcutsTab />);
    const sendRecorder = screen.getByRole("button", {
      name: "Change shortcut for Send",
    });

    fireEvent.click(sendRecorder);
    fireEvent.keyDown(sendRecorder, { key: "Enter", shiftKey: true });
    expect(getShortcut("send")).toEqual(
      createShortcutChord("Enter", { primary: true }),
    );
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Send can only use Enter or Cmd/Ctrl+Enter.",
    );

    fireEvent.keyDown(sendRecorder, { key: "Enter", ctrlKey: true });
    expect(getShortcut("send")).toEqual(
      createShortcutChord("Enter", { primary: true }),
    );
  });

  it("rejects shortcuts already assigned to another action", () => {
    renderWithI18n(<KeyboardShortcutsTab />);
    const recorder = screen.getByRole("button", {
      name: "Change shortcut for Create issue",
    });

    fireEvent.click(recorder);
    fireEvent.keyDown(recorder, { key: "k", ctrlKey: true });

    expect(getShortcut("createIssue")).toEqual(createShortcutChord("C"));
    expect(screen.getByRole("alert")).toHaveTextContent(
      "Already used by Open search.",
    );
  });

  it("can disable and restore an action", () => {
    renderWithI18n(<KeyboardShortcutsTab />);

    fireEvent.click(
      screen.getByRole("button", {
        name: "Disable Create issue shortcut",
      }),
    );
    expect(getShortcut("createIssue")).toBeNull();

    fireEvent.click(
      screen.getByRole("button", { name: "Reset Create issue" }),
    );
    expect(getShortcut("createIssue")).toEqual(createShortcutChord("C"));
  });

  it("confirms before restoring all shortcut defaults", () => {
    useShortcutStore.getState().setShortcut(
      "openSearch",
      createShortcutChord("J", { primary: true }),
    );
    renderWithI18n(<KeyboardShortcutsTab />);

    fireEvent.click(screen.getByRole("button", { name: "Restore defaults" }));
    expect(screen.getByRole("alertdialog")).toHaveTextContent(
      "This will remove every custom shortcut and restore the defaults on this device.",
    );
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("J", { primary: true }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("J", { primary: true }),
    );

    fireEvent.click(screen.getByRole("button", { name: "Restore defaults" }));
    fireEvent.click(
      screen.getByRole("button", { name: "Restore all defaults" }),
    );
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(getShortcut("openSearch")).toEqual(
      createShortcutChord("K", { primary: true }),
    );
  });

  describe("sequences", () => {
    const sequence = (first: string, then: string) => ({
      ...createShortcutChord(first),
      then: createShortcutChord(then),
    });
    const recorderFor = (action: string) =>
      screen.getByRole("button", { name: `Change shortcut for ${action}` });

    it("shows the default G then I with its separator", () => {
      renderWithI18n(<KeyboardShortcutsTab />);
      expect(within(recorderFor("Go to Inbox")).getByText("then")).toBeInTheDocument();
      expect(getShortcut("goInbox")).toEqual(sequence("G", "I"));
    });

    it("records a navigation action's two keys one after the other", () => {
      renderWithI18n(<KeyboardShortcutsTab />);
      const recorder = recorderFor("Go to Autopilot");
      expect(recorder).not.toHaveAttribute("data-shortcut-recorder");
      fireEvent.click(recorder);
      expect(recorder).toHaveAttribute("data-shortcut-recorder");
      fireEvent.keyDown(recorder, { key: "g" });

      expect(getShortcut("goAutopilots")).toBeNull();
      expect(recorder).toHaveTextContent("then…");
      expect(screen.getByText(/Press the second key/)).toBeInTheDocument();

      fireEvent.keyDown(recorder, { key: "u" });
      expect(getShortcut("goAutopilots")).toEqual(sequence("G", "U"));
    });

    it("keeps the first key alone on Enter, and starts over on Backspace", () => {
      renderWithI18n(<KeyboardShortcutsTab />);
      const recorder = recorderFor("Go to Autopilot");
      fireEvent.click(recorder);
      fireEvent.keyDown(recorder, { key: "o" });
      fireEvent.keyDown(recorder, { key: "Backspace" });
      expect(recorder).toHaveTextContent("Press keys");

      fireEvent.keyDown(recorder, { key: "o" });
      fireEvent.keyDown(recorder, { key: "Enter" });
      expect(getShortcut("goAutopilots")).toEqual(createShortcutChord("O"));
    });

    it("refuses a key that starts another action's sequence, and a sequence after another's key", () => {
      renderWithI18n(<KeyboardShortcutsTab />);
      const recorder = recorderFor("Create issue");
      fireEvent.click(recorder);
      fireEvent.keyDown(recorder, { key: "g" });
      expect(screen.getByRole("alert")).toHaveTextContent("Already used by Go to Inbox.");
      expect(getShortcut("createIssue")).toEqual(createShortcutChord("C"));
      fireEvent.keyDown(recorder, { key: "Escape" });

      const autopilot = recorderFor("Go to Autopilot");
      fireEvent.click(autopilot);
      fireEvent.keyDown(autopilot, { key: "c" });
      fireEvent.keyDown(autopilot, { key: "x" });
      expect(screen.getByRole("alert")).toHaveTextContent("Already used by Create issue.");
      expect(getShortcut("goAutopilots")).toBeNull();
    });

    it("refuses a sequence that is already taken", () => {
      renderWithI18n(<KeyboardShortcutsTab />);
      const recorder = recorderFor("Go to Autopilot");
      fireEvent.click(recorder);
      fireEvent.keyDown(recorder, { key: "g" });
      fireEvent.keyDown(recorder, { key: "i" });
      expect(screen.getByRole("alert")).toHaveTextContent("Already used by Go to Inbox.");
    });

    it("records other actions' single keys at once", () => {
      renderWithI18n(<KeyboardShortcutsTab />);
      const recorder = recorderFor("Create issue");
      fireEvent.click(recorder);
      fireEvent.keyDown(recorder, { key: "n" });
      expect(getShortcut("createIssue")).toEqual(createShortcutChord("N"));
    });
  });
});
