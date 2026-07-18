import { useState } from "react";
import { describe, it, expect, vi } from "vitest";
import { screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { SupportedLocale } from "@multica/core/i18n";
import { renderWithI18n } from "../../../test/i18n";
import { ScheduleEditor } from "./schedule-editor";
import type { ScheduleConfig } from "./model";
import { getDefaultScheduleConfig } from "./model";
import { cronFields, parseCron, toCron } from "./cron-mapping";

// The heaviest interaction cases drive several async userEvent steps through
// Base UI dropdowns; fast locally, but a loaded CI runner can push a single one
// past the default 5s cap. Raise the ceiling without slowing the fast cases.
vi.setConfig({ testTimeout: 15_000 });

// The preview is server-owned; the editor must never approximate it. The mock
// stands in for the cron-preview endpoint: it 400s on anything that isn't a
// 5-field expression (mirroring the robfig parser's contract) and can be told
// to fail at the transport level, which must NOT read as "your cron is wrong".
const previewFailure = {
  transport: false,
  unreadable: false,
  badTimezone: false,
  // A 400 whose code the editor does not know. The endpoint only ever sends
  // invalid_cron and invalid_timezone today, but a rejection it cannot classify
  // must still reach the user with the server's own words attached.
  unknownCode: false,
  // Every run already in the past, so the editor's "the list has expired,
  // refresh it" effect fires. Two schedules sharing a next-run instant is the
  // norm, not a coincidence — anything firing at tomorrow 09:00 does.
  expired: false,
};
// Every expression the editor actually asked the server about, in order. The
// expired-preview refresh is an *extra* fetch on top of the first one, so its
// absence for a given expression is visible as a missing entry here.
const previewCalls: string[] = [];

vi.mock("@multica/core/autopilots/queries", () => ({
  cronPreviewOptions: (
    wsId: string,
    expr: string,
    tz: string,
    options?: { enabled?: boolean },
  ) => ({
    queryKey: [
      "autopilots",
      wsId,
      "cron-preview",
      expr,
      tz,
      previewFailure.transport,
      previewFailure.unreadable,
      previewFailure.badTimezone,
      previewFailure.unknownCode,
      previewFailure.expired,
    ],
    queryFn: async () => {
      previewCalls.push(expr);
      if (previewFailure.expired) {
        // A shared instant, deliberately: the guard must tell the two
        // expressions apart, and it cannot do that from the timestamp alone.
        return { next_runs: ["2020-01-01T00:00:00Z"] };
      }
      if (previewFailure.transport) {
        throw new ApiError("API error: 500 Internal Server Error", 500, "Internal Server Error");
      }
      if (previewFailure.badTimezone) {
        throw new ApiError(`invalid timezone "${tz}"`, 400, "Bad Request", {
          error: `invalid timezone "${tz}"`,
          code: "invalid_timezone",
        });
      }
      if (previewFailure.unknownCode) {
        throw new ApiError("schedule rejected by policy", 400, "Bad Request", {
          error: "schedule rejected by policy",
          code: "some_future_code",
        });
      }
      // The server reads a TZ=/CRON_TZ= prefix off the expression itself
      // (case-sensitively, up to the first literal space) and validates the
      // embedded zone before the fields — the guarded no-space shape and an
      // unknown zone are both invalid_cron, since they are part of the string.
      let fields = expr;
      if (fields.startsWith("TZ=") || fields.startsWith("CRON_TZ=")) {
        const space = fields.indexOf(" ");
        const zone = space === -1 ? null : fields.slice(fields.indexOf("=") + 1, space);
        // "asia/shanghai" mirrors a dev server on a case-insensitive filesystem,
        // where LoadLocation takes the lowercase spelling too.
        if (zone === null || !["UTC", "Asia/Tokyo", "Asia/Shanghai", "asia/shanghai", "Local", ""].includes(zone)) {
          throw new ApiError(`parse cron: provided bad location ${zone ?? expr}`, 400, "Bad Request", {
            error: "parse cron: provided bad location",
            code: "invalid_cron",
          });
        }
        fields = fields.slice(space).trim();
      }
      if (fields.trim().split(/\s+/).length !== 5) {
        throw new ApiError("parse cron: expected exactly 5 fields", 400, "Bad Request", {
          error: "parse cron: expected exactly 5 fields",
          code: "invalid_cron",
        });
      }
      // A response the schema could not read degrades to null — distinct from
      // an empty list, which means "this expression never fires".
      if (previewFailure.unreadable) return { next_runs: null };
      if (fields === "0 0 30 2 *") return { next_runs: [] };
      return {
        next_runs: ["2126-07-14T01:00:00Z", "2126-07-14T03:00:00Z", "2126-07-14T05:00:00Z"],
      };
    },
    enabled: options?.enabled ?? true,
    retry: false,
  }),
}));

vi.mock("../pickers/timezone-picker", () => ({
  TimezonePicker: ({ value, onChange }: { value: string; onChange: (tz: string) => void }) => (
    <select data-testid="timezone-picker" value={value} onChange={(e) => onChange(e.target.value)}>
      <option value="UTC">UTC</option>
      <option value="Asia/Shanghai">Asia/Shanghai</option>
    </select>
  ),
}));

function Harness({
  initial,
  onChange,
  disabled,
}: {
  initial: ScheduleConfig;
  onChange?: () => void;
  disabled?: boolean;
}) {
  const [value, setValue] = useState(initial);
  const [valid, setValid] = useState(true);
  return (
    <>
      <ScheduleEditor
        value={value}
        onChange={(next) => {
          onChange?.();
          setValue(next);
        }}
        wsId="ws-test"
        disabled={disabled}
        onValidityChange={setValid}
      />
      <output data-testid="cron-out">{cronFields(value)}</output>
      <output data-testid="wire-out">{toCron(value)}</output>
      <output data-testid="raw-out">{String(value.raw)}</output>
      <output data-testid="tz-out">{value.timezone}</output>
      <output data-testid="valid-out">{String(valid)}</output>
    </>
  );
}

function renderEditor(
  initial: ScheduleConfig,
  opts?: { onChange?: () => void; disabled?: boolean; locale?: SupportedLocale },
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <Harness initial={initial} onChange={opts?.onChange} disabled={opts?.disabled} />
    </QueryClientProvider>,
    opts?.locale === undefined ? undefined : { locale: opts.locale },
  );
}

const cron = (expr: string) => parseCron(expr, "UTC");
const cronOut = () => screen.getByTestId("cron-out").textContent;
const wireOut = () => screen.getByTestId("wire-out").textContent;

// The cron expression is read-only until clicked, but already an input in
// advanced-only mode, so only click it open when the field isn't mounted yet.
async function openCronInput(): Promise<HTMLElement> {
  if (screen.queryByRole("textbox", { name: "Cron" }) === null) {
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: /click to edit/ }));
  }
  return screen.getByRole("textbox", { name: "Cron" });
}

async function editCronText(expr: string) {
  const input = await openCronInput();
  fireEvent.change(input, { target: { value: expr } });
  fireEvent.blur(input);
}

describe("ScheduleEditor", () => {
  it("renders the three form blocks and the cron readback", () => {
    renderEditor(cron("0 9-21 * * *"));
    expect(screen.getByText("Time")).toBeInTheDocument();
    expect(screen.getByText("Days")).toBeInTheDocument();
    expect(screen.getByText("Timezone")).toBeInTheDocument();
    // The cron expression lives in the result panel below the form, shown as a
    // click-to-edit readback rather than a labelled field.
    expect(screen.getByRole("button", { name: /click to edit/ })).toBeInTheDocument();
  });

  it("never fires onChange on mount (untouched save sends no update)", async () => {
    const onChange = vi.fn();
    renderEditor(cron("0 9-21 * * *"), { onChange });
    await waitFor(() => expect(screen.getByText("Next runs")).toBeInTheDocument());
    expect(onChange).not.toHaveBeenCalled();
    expect(cronOut()).toBe("0 9-21 * * *");
  });

  it("echoes a compound expression into structured controls", () => {
    renderEditor(cron("0 9-21/2 * * 2-4"));
    expect(screen.getByTestId("raw-out").textContent).toBe("null");
    expect(screen.getByRole("button", { name: "Tuesday", pressed: true })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Thursday", pressed: true })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Monday", pressed: false })).toBeInTheDocument();
    expect(screen.getByText(/Every 2 hours · 09:00–21:00 · Tue–Thu/)).toBeInTheDocument();
  });

  it("toggles weekly day chips without collapsing the selection", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9 * * 2-4"));
    await user.click(screen.getByRole("button", { name: "Friday" }));
    expect(cronOut()).toBe("0 9 * * 2-5");
    await user.click(screen.getByRole("button", { name: "Tuesday" }));
    expect(cronOut()).toBe("0 9 * * 3-5");
  });

  it("keeps a compound time pattern intact while the days are edited", async () => {
    const user = userEvent.setup();
    // Every other day-editing test starts from 09:00 — the default time — so a
    // setDays that reset the time dimension would pass them all unnoticed. This
    // one starts where the time is nothing like the default.
    renderEditor(cron("30 9-21/3 * * 2-4"));
    await user.click(screen.getByRole("button", { name: "Friday" }));
    expect(cronOut()).toBe("30 9-21/3 * * 2-5");
    await user.click(screen.getByRole("button", { name: "Tuesday" }));
    expect(cronOut()).toBe("30 9-21/3 * * 3-5");

    // …and the same for a switch of day *kind*, not just a chip toggle.
    await user.click(screen.getByLabelText("Day pattern"));
    await user.click(await screen.findByRole("option", { name: "Day of month" }));
    expect(cronOut()).toBe("30 9-21/3 1 * *");
    await user.click(screen.getByLabelText("Day pattern"));
    await user.click(await screen.findByRole("option", { name: "Every day" }));
    expect(cronOut()).toBe("30 9-21/3 * * *");
  });

  it("edits interval, window and day chips in sequence on one schedule", async () => {
    const user = userEvent.setup();
    // The three dimensions are written by two handlers that each overwrite the
    // whole config (`{...value, time | days, raw: null}`). Editing them one at a
    // time from a trivial baseline never shows one clobbering another.
    renderEditor(cron("0 9-21/2 * * 2-4"));

    fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "3" } });
    expect(cronOut()).toBe("0 9-21/3 * * 2-4");

    await user.click(screen.getByLabelText("Window end hour"));
    await user.keyboard("18");
    expect(cronOut()).toBe("0 9-18/3 * * 2-4");

    await user.click(screen.getByRole("button", { name: "Monday" }));
    expect(cronOut()).toBe("0 9-18/3 * * 1-4");

    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("10");
    expect(cronOut()).toBe("0 10-18/3 * * 1-4");
  });

  it("keeps at least one weekly day selected", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9 * * 1"));
    await user.click(screen.getByRole("button", { name: "Monday" }));
    expect(cronOut()).toBe("0 9 * * 1");
  });

  // Stepping off a bound wraps; stepping from OUTSIDE the range lands on the
  // bound that was overshot. Clamping before stepping instead of after made the
  // bounds themselves unreachable from a mistyped number.
  it.each([
    ["0", "ArrowUp", "0 9 1 * *"],
    ["0", "ArrowDown", "0 9 1 * *"],
    ["40", "ArrowDown", "0 9 31 * *"],
    ["40", "ArrowUp", "0 9 31 * *"],
  ])("steps a mistyped day-of-month %s back onto the bound it overshot", (typed, key, expected) => {
    renderEditor(cron("0 9 15 * *"));
    const box = screen.getByRole("spinbutton");
    fireEvent.change(box, { target: { value: typed } });
    fireEvent.keyDown(box, { key });
    expect(cronOut()).toBe(expected);
  });

  it.each([
    ["31", "ArrowUp", "0 9 1 * *"],
    ["1", "ArrowDown", "0 9 31 * *"],
  ])("still wraps the day-of-month at %s", (typed, key, expected) => {
    renderEditor(cron("0 9 15 * *"));
    const box = screen.getByRole("spinbutton");
    fireEvent.change(box, { target: { value: typed } });
    fireEvent.keyDown(box, { key });
    expect(cronOut()).toBe(expected);
  });

  it("edits the interval step outside of raw cron", () => {
    renderEditor(cron("0 */2 * * *"));
    fireEvent.change(screen.getByRole("spinbutton"), { target: { value: "3" } });
    expect(cronOut()).toBe("0 */3 * * *");
  });

  it("hydrates structured controls from cron text", async () => {
    renderEditor(cron("0 9 * * *"));
    await editCronText("0 9-21/2 * * 2-4");
    expect(cronOut()).toBe("0 9-21/2 * * 2-4");
    expect(screen.getByTestId("raw-out").textContent).toBe("null");
    expect(screen.getByRole("button", { name: "Tuesday", pressed: true })).toBeInTheDocument();
  });

  it("enters advanced-only mode for unrepresentable expressions and recovers", async () => {
    renderEditor(cron("0 9 1,15 * *"));
    // The notice waits for the server to take the expression — until then all the
    // editor knows is that it cannot parse it, which is also true of a typo.
    await waitFor(() =>
      expect(screen.getByText(/visual editor can't represent/)).toBeInTheDocument(),
    );
    expect(screen.getByTestId("raw-out").textContent).toBe("0 9 1,15 * *");

    await editCronText("0 9 * * *");
    expect(screen.getByTestId("raw-out").textContent).toBe("null");
    expect(screen.queryByText(/visual editor can't represent/)).not.toBeInTheDocument();
  });

  it("takes a degenerate expression into the controls instead of greying them out", async () => {
    // "*/65" steps past the minute field on its first stride, so it selects
    // minute 0 — the 14th of the month, on the hour, from 10:00 to 20:59. Every
    // control can hold that, and the editor used to claim otherwise while the
    // preview below it listed the hourly runs.
    renderEditor(cron("*/65 10-20 14 * *"));
    expect(screen.getByTestId("raw-out").textContent).toBe("null");
    expect(screen.getByTestId("cron-out").textContent).toBe("0 10-20 14 * *");
    expect(screen.queryByText(/visual editor can't represent/)).not.toBeInTheDocument();
    // Live controls, carrying the schedule — not a greyed-out shell over a raw
    // cron box.
    expect(screen.getByLabelText("Day of month")).toHaveValue(14);
  });

  it("changes the timezone of a structured schedule without touching the expression", async () => {
    renderEditor(cron("0 9-21/2 * * 2-4"));
    // The zone has to change on a *settled* preview, or "not settled yet" and
    // "re-querying for the new zone" are the same state and prove nothing.
    const line = await waitFor(() => {
      const el = screen.getByText("Next runs").closest("div");
      expect(el).toHaveAttribute("aria-busy", "false");
      return el;
    });

    fireEvent.change(screen.getByTestId("timezone-picker"), {
      target: { value: "Asia/Shanghai" },
    });
    expect(screen.getByTestId("tz-out").textContent).toBe("Asia/Shanghai");
    // The timezone is orthogonal to the expression: it is the one control that
    // must move the schedule without rewriting a single cron field.
    expect(cronOut()).toBe("0 9-21/2 * * 2-4");
    expect(screen.getByTestId("raw-out").textContent).toBe("null");
    // The preview is the server's answer for a cron AND a zone, so a new zone
    // must re-ask: 09:00 in Shanghai is not 09:00 in UTC. A preview keyed on the
    // expression alone would still read settled here, having never noticed.
    expect(line).toHaveAttribute("aria-busy", "true");
    await waitFor(() => expect(line).toHaveAttribute("aria-busy", "false"));
  });

  it("keeps the timezone editable in advanced-only mode and preserves raw", () => {
    renderEditor(cron("0 9 1,15 * *"));
    fireEvent.change(screen.getByTestId("timezone-picker"), {
      target: { value: "Asia/Shanghai" },
    });
    expect(screen.getByTestId("tz-out").textContent).toBe("Asia/Shanghai");
    expect(screen.getByTestId("raw-out").textContent).toBe("0 9 1,15 * *");
  });

  it("shows server-driven next runs", async () => {
    renderEditor(getDefaultScheduleConfig("UTC"));
    await waitFor(() => expect(screen.getByText("Next runs")).toBeInTheDocument());
  });

  it("shows the server error inline for an invalid expression", async () => {
    renderEditor(cron("0 9 * * *"));
    await editCronText("@daily");
    await waitFor(() => expect(screen.getByText(/expected exactly 5 fields/)).toBeInTheDocument());
  });

  it("does not accept an emptied cron expression", async () => {
    renderEditor(cron("0 9 * * 1-5"));
    await editCronText("   ");
    // Snaps back to the model instead of writing "" into raw and letting the
    // dialog submit an empty cron_expression.
    expect(screen.getByTestId("cron-out").textContent).toBe("0 9 * * 1-5");
    expect(screen.getByTestId("raw-out").textContent).toBe("null");
  });

  it("does not reinterpret the cron text while the user is still typing", async () => {
    renderEditor(cron("0 9 * * *"));
    const input = await openCronInput();
    fireEvent.change(input, { target: { value: "0 9 * *" } });
    // Half-typed text must not flip the editor into advanced-only mode; the
    // model only changes on blur/Enter.
    expect(screen.getByTestId("raw-out").textContent).toBe("null");
    expect(screen.getByRole("button", { name: "At a time" })).toBeEnabled();
  });

  it("validates the cron text on blur, not on every keystroke", async () => {
    renderEditor(cron("0 9 * * *"));
    const input = await openCronInput();
    fireEvent.change(input, { target: { value: "0 9 * *" } });
    // The half-typed expression would be rejected by the server, but it is not
    // the user's answer yet — nothing is sent and nothing is flagged.
    await waitFor(() => expect(screen.getByText("Next runs")).toBeInTheDocument());
    expect(screen.queryByText(/isn't valid/)).not.toBeInTheDocument();

    fireEvent.blur(input);
    await waitFor(() => expect(screen.getByText(/isn't valid/)).toBeInTheDocument());
  });

  it("commits the cron text on Enter", async () => {
    renderEditor(cron("0 9 * * *"));
    const input = await openCronInput();
    fireEvent.change(input, { target: { value: "0 9-21/2 * * 2-4" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByTestId("cron-out").textContent).toBe("0 9-21/2 * * 2-4");
  });

  it("disables the structured controls in advanced-only mode, not just greys them", () => {
    renderEditor(cron("0 9 1,15 * *"));
    // pointer-events-none alone would still let a keyboard user activate these
    // and silently overwrite the hand-written expression.
    expect(screen.getByRole("button", { name: "At a time" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "At an interval" })).toBeDisabled();
    expect(screen.getByLabelText("Hour")).toBeDisabled();
  });

  it("disables every schedule control when the editor is locked", async () => {
    renderEditor(cron("0 9 * * 1-5"), { disabled: true });
    expect(screen.getByRole("button", { name: "At a time" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Monday" })).toBeDisabled();
    // Locked: the cron row cannot even be opened for editing.
    expect(screen.getByRole("button", { name: /click to edit/ })).toBeDisabled();
    expect(screen.queryByRole("textbox", { name: "Cron" })).not.toBeInTheDocument();
  });

  it("still shows next runs when the editor is locked", async () => {
    renderEditor(cron("0 9 * * 1-5"), { disabled: true });
    await waitFor(() => expect(screen.getByText("Next runs")).toBeInTheDocument());
  });

  it("keeps the hour when switching between fixed time and interval", async () => {
    const user = userEvent.setup();
    renderEditor(cron("30 14 * * *"));
    await user.click(screen.getByRole("button", { name: "At an interval" }));
    await user.click(screen.getByRole("button", { name: "At a time" }));
    // The interval model has nowhere to keep an hour; losing it would silently
    // rewrite 14:30 as 09:30.
    expect(screen.getByTestId("cron-out").textContent).toBe("30 14 * * *");
  });

  it("keeps the minute across a fixed time → minute-step window → fixed time trip", async () => {
    const user = userEvent.setup();
    renderEditor(cron("30 14 * * *"));
    await user.click(screen.getByRole("button", { name: "At an interval" }));
    await user.click(screen.getByLabelText("Interval unit"));
    await user.click(await screen.findByRole("option", { name: "minutes" }));
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("14");
    await user.click(screen.getByRole("button", { name: "At a time" }));
    // A minute-step window is hour-granular, so its bounds read :00 — but the
    // model still holds the 30 the user typed. Reading the anchor back off the
    // window would silently fire the schedule half an hour early.
    expect(screen.getByTestId("cron-out").textContent).toBe("30 14 * * *");
  });

  it("focuses the interval when the time switches to an interval", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9 * * *"));
    await user.click(screen.getByRole("button", { name: "At an interval" }));
    // The switch reveals a fresh field; the caret lands in it so the step can be
    // typed without a second click.
    expect(screen.getByLabelText("Interval")).toHaveFocus();
  });

  it("focuses the hour when the time switches back to a fixed time", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 */2 * * *"));
    await user.click(screen.getByRole("button", { name: "At a time" }));
    expect(screen.getByLabelText("Hour")).toHaveFocus();
  });

  it("focuses the step when the interval unit switches", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 */2 * * *"));
    await user.click(screen.getByLabelText("Interval unit"));
    await user.click(await screen.findByRole("option", { name: "minutes" }));
    // The step stays mounted across the unit switch, so this is an imperative
    // focus, not autoFocus — the caret returns to the number to retype it.
    expect(screen.getByLabelText("Interval")).toHaveFocus();
  });

  it("focuses the day box when the day pattern switches to monthly", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9 * * *"));
    await user.click(screen.getByLabelText("Day pattern"));
    await user.click(await screen.findByRole("option", { name: "Day of month" }));
    expect(screen.getByLabelText("Day of month")).toHaveFocus();
  });

  it("does not steal focus on first render, even when a field is already shown", async () => {
    // The gate is a user switch, not the field's presence: a schedule that opens
    // already monthly must not pull focus (and the panel's scroll) on mount.
    renderEditor(cron("0 9 14 * *"));
    expect(screen.getByLabelText("Day of month")).not.toHaveFocus();
  });

  it("names every control, so none is announced as a bare box", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9 * * *"));
    expect(screen.getByLabelText("Hour")).toBeInTheDocument();
    expect(screen.getByLabelText("Minute")).toBeInTheDocument();
    expect(screen.getByLabelText("Day pattern")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "At an interval" }));
    expect(screen.getByLabelText("Interval")).toBeInTheDocument();
    expect(screen.getByLabelText("Interval unit")).toBeInTheDocument();
    // The window is on screen from the moment the interval is, all-day included.
    expect(screen.getByLabelText("Window start hour")).toBeInTheDocument();
    expect(screen.getByLabelText("Window end hour")).toBeInTheDocument();

    await user.click(screen.getByLabelText("Day pattern"));
    await user.click(await screen.findByRole("option", { name: "Day of month" }));
    expect(screen.getByLabelText("Day of month")).toBeInTheDocument();
  });

  it("labels every select trigger with its option's text, not its raw value", async () => {
    renderEditor(cron("*/15 9-21 * * *"));
    // Base UI falls back to the raw value, which would read as "every" and
    // "minutes" — the enum members, not the labels the user picked.
    const triggers = screen.getAllByRole("combobox").map((el) => el.textContent ?? "");
    expect(triggers.some((text) => text.startsWith("Every day"))).toBe(true);
    expect(triggers.some((text) => text.startsWith("minutes"))).toBe(true);
  });

  it("edits both ends of a minute-step window in place, with no minute segment", async () => {
    const user = userEvent.setup();
    renderEditor(cron("*/15 9-21 * * *"));
    const startHour = screen.getByLabelText("Window start hour");
    const endHour = screen.getByLabelText("Window end hour");
    // A minute-step window is hour-granular: the bounds carry no minute, so the
    // minute segment is not rendered at all — not shown greyed-out.
    expect(screen.queryByLabelText("Window start minute")).toBeNull();
    expect(screen.queryByLabelText("Window end minute")).toBeNull();
    await user.click(startHour!);
    await user.keyboard("10");
    expect(cronOut()).toBe("*/15 10-21 * * *");
    await user.click(endHour!);
    await user.keyboard("18");
    expect(cronOut()).toBe("*/15 10-18 * * *");
  });

  it("restores the interval and its window after a round trip through fixed time", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-21/3 * * 1-5"));
    await user.click(screen.getByRole("button", { name: "At a time" }));
    await user.click(screen.getByRole("button", { name: "At an interval" }));
    // Step, unit and window are as much user input as the hour is; rebuilding
    // the pattern from defaults would silently turn "every 3h, 9-21" into
    // "every hour, all day".
    expect(screen.getByTestId("cron-out").textContent).toBe("0 9-21/3 * * 1-5");
  });

  it("reports a transport failure as an unavailable preview, not an invalid cron", async () => {
    previewFailure.transport = true;
    try {
      renderEditor(cron("0 9 * * *"));
      await waitFor(() => expect(screen.getByText(/Next runs unavailable/)).toBeInTheDocument());
      expect(screen.queryByText(/500 Internal Server Error/)).not.toBeInTheDocument();
    } finally {
      previewFailure.transport = false;
    }
  });

  it("says so when a valid expression has no upcoming runs", async () => {
    renderEditor(cron("0 0 30 2 *"));
    await waitFor(() => expect(screen.getByText(/no upcoming runs/)).toBeInTheDocument());
  });

  it("does not call an unreadable response 'no upcoming runs'", async () => {
    previewFailure.unreadable = true;
    try {
      renderEditor(cron("0 9 * * *"));
      await waitFor(() => expect(screen.getByText(/Next runs unavailable/)).toBeInTheDocument());
      // A drifted response must never be presented as "your daily 09:00
      // schedule will never run".
      expect(screen.queryByText(/no upcoming runs/)).not.toBeInTheDocument();
    } finally {
      previewFailure.unreadable = false;
    }
  });

  it("reports validity so the dialog can block an invalid cron", async () => {
    renderEditor(cron("0 9 * * *"));
    await waitFor(() => expect(screen.getByTestId("valid-out").textContent).toBe("true"));
    await editCronText("@daily");
    await waitFor(() => expect(screen.getByTestId("valid-out").textContent).toBe("false"));
    expect(screen.getByText("This cron expression isn't valid.")).toBeInTheDocument();
    // A rejected expression has no runs: the section goes entirely, rather than
    // leaving a heading and a divider framing nothing under the error.
    expect(screen.queryByText("Next runs")).not.toBeInTheDocument();
  });

  it("keeps a transport failure from marking the expression invalid", async () => {
    previewFailure.transport = true;
    try {
      renderEditor(cron("0 9 * * *"));
      await waitFor(() => expect(screen.getByText(/Next runs unavailable/)).toBeInTheDocument());
      expect(screen.getByTestId("valid-out").textContent).toBe("true");
    } finally {
      previewFailure.transport = false;
    }
  });

  it("types the window end two digits at a time without clamping mid-keystroke", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-21 * * *"));
    const endHour = screen.getByLabelText("Window end hour");
    await user.click(endHour!);
    // The intermediate "01" is below the start hour. Clamping it there would
    // leave TimeInput building the second digit from a value it never showed.
    await user.keyboard("12");
    expect(cronOut()).toBe("0 9-12 * * *");
  });

  it("never lets the window end fall below its start, not even mid-edit", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-21 * * *"));
    const endHour = screen.getByLabelText("Window end hour");
    await user.click(endHour!);
    await user.keyboard("08");
    // A window that ends before it starts has no cron form. The two ends
    // constrain each other while typing, so it is never displayed or submitted —
    // no error at save time.
    expect(endHour).toHaveValue("09");
    expect(cronOut()).toBe("0 9-9 * * *");
  });

  it("draws the interval and its unit in one box, lit by whichever holds focus", async () => {
    renderEditor(cron("0 */3 * * *"));
    const step = screen.getByLabelText("Interval");
    const unit = screen.getByLabelText("Interval unit");
    // "Every 3 hours" is one setting: the step and the unit share a box, rather
    // than sitting in two that happen to be next to each other.
    const box = step.closest("[data-slot=input-group]");
    expect(box).not.toBeNull();
    expect(box).toContainElement(unit);
    // The box lights its border from the control inside it that has focus, and it
    // knows them by this slot. A trigger that kept its own would leave the box
    // dark while the select it holds is the very thing focused.
    expect(step).toHaveAttribute("data-slot", "input-group-control");
    expect(unit).toHaveAttribute("data-slot", "input-group-control");
  });

  it("draws the window's two ends in one box", async () => {
    renderEditor(cron("0 9-21/3 * * *"));
    const start = screen.getByLabelText("Window start hour");
    // A window is one value with two ends, so it reads as one control rather than
    // two fields that happen to sit next to each other.
    const box = start.closest("[data-slot=input-group]");
    expect(box).not.toBeNull();
    expect(box).toContainElement(screen.getByLabelText("Window end hour"));
    // The segments, not their wrapper, are what the box lights its border from:
    // they are what gets focused.
    expect(start).toHaveAttribute("data-slot", "input-group-control");
  });

  // The window is a dimension of the schedule, not a mode of the editor: it is on
  // screen for every interval, and narrowing it is one edit — not a mode switch
  // followed by one.
  it("shows the day's bounds for an all-day interval, at either unit", async () => {
    const user = userEvent.setup();
    renderEditor(cron("15 */3 * * *"));
    // The hours unit carries the firing minute at both ends.
    expect(screen.getByLabelText("Window start hour")).toHaveValue("00");
    expect(screen.getByLabelText("Window start minute")).toHaveValue("15");
    expect(screen.getByLabelText("Window end hour")).toHaveValue("23");
    expect(screen.getByLabelText("Window end minute")).toHaveValue("15");

    await user.click(screen.getByLabelText("Interval unit"));
    await user.click(await screen.findByRole("option", { name: "minutes" }));
    // A minute-step window is hour-granular: the bounds are the whole hours.
    expect(screen.getByLabelText("Window start hour")).toHaveValue("00");
    expect(screen.getByLabelText("Window end hour")).toHaveValue("23");
  });

  it("narrows an all-day interval by editing the bound, with nothing to click first", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 */3 * * *"));
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("09");
    // One edit, no mode switch.
    expect(cronOut()).toBe("0 9-23/3 * * *");
  });

  it("takes a window widened back to the whole day as all day again", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-21/3 * * *"));
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("00");
    await user.click(screen.getByLabelText("Window end hour"));
    await user.keyboard("23");
    // A window that spans the day IS all day: the schedule has one form, and
    // "0-23/3" is not a second one.
    expect(cronOut()).toBe("0 */3 * * *");
  });

  it("clears the window end to the end of the day, not onto its start", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-15 * * *"));
    await user.click(screen.getByLabelText("Window end hour"));
    await user.keyboard("{Backspace}");
    // The end's floor is wherever the start sits, but that is not its neutral
    // value: clearing it opens the window to the end of the day.
    expect(cronOut()).toBe("0 9-23 * * *");
  });

  it("clears the window start to midnight", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-23 * * *"));
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("{Backspace}");
    // Both bounds cleared is the whole day — which is all day, not a 00-23 window.
    expect(cronOut()).toBe("0 * * * *");
  });

  it("raises the window end when the start is moved past it", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-12 * * *"));
    const startHour = screen.getByLabelText("Window start hour");
    await user.click(startHour!);
    await user.keyboard("18");
    expect(cronOut()).toBe("0 18-18 * * *");
  });

  it("moves the shared firing minute when the window end's minute is edited", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-21 * * *"));
    const endMinute = screen.getByLabelText("Window end minute");
    await user.click(endMinute!);
    await user.keyboard("30");
    // cron keeps one minute for both ends of the range, so the start moves too.
    expect(cronOut()).toBe("30 9-21 * * *");
  });

  it("keeps the firing minute across an interval unit round-trip", async () => {
    const user = userEvent.setup();
    renderEditor(cron("30 */2 * * *"));
    await user.click(screen.getAllByRole("combobox")[0]!);
    await user.click(await screen.findByRole("option", { name: "minutes" }));
    await user.click(screen.getAllByRole("combobox")[0]!);
    await user.click(await screen.findByRole("option", { name: "hours" }));
    expect(cronOut()).toBe("30 */2 * * *");
  });

  it("keeps the firing minute across a unit round-trip with a time window", async () => {
    const user = userEvent.setup();
    renderEditor(cron("30 9-21/2 * * *"));
    await user.click(screen.getAllByRole("combobox")[0]!);
    await user.click(await screen.findByRole("option", { name: "minutes" }));
    await user.click(screen.getAllByRole("combobox")[0]!);
    await user.click(await screen.findByRole("option", { name: "hours" }));
    // The window carries the firing minute in cron, so the unit toggle must not
    // strand it: `minute`, not the window text, is the source of truth.
    expect(cronOut()).toBe("30 9-21/2 * * *");
  });

  it("keeps the firing minute when the window is widened back to the whole day", async () => {
    const user = userEvent.setup();
    renderEditor(cron("30 9-21/2 * * *"));
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("00");
    await user.click(screen.getByLabelText("Window end hour"));
    await user.keyboard("23");
    // The minute is the schedule's, not the window's: dropping it when the window
    // goes would move the runs to the top of the hour behind the user's back.
    expect(cronOut()).toBe("30 */2 * * *");
  });

  it("keeps both typed digits when the window start's minute is edited", async () => {
    const user = userEvent.setup();
    renderEditor(cron("1 9-21/2 * * *"));
    const startMinute = screen.getByLabelText("Window start minute");
    await user.click(startMinute!);
    // The second keystroke must build on the first one, not on the minute the
    // model held before it — a stale minute would snap "12" back to "01".
    await user.keyboard("12");
    expect(cronOut()).toBe("12 9-21/2 * * *");
  });

  it("never leaves a cleared number box sitting over a stale model value", async () => {
    const user = userEvent.setup();
    renderEditor(cron("*/15 * * * *"));
    const interval = screen.getByRole("spinbutton");
    await user.clear(interval);
    // While the box is empty the model keeps the last valid value, but the box
    // must not stay empty over it once the user leaves the field.
    expect(cronOut()).toBe("*/15 * * * *");
    fireEvent.blur(interval);
    expect(interval).toHaveValue(15);
  });

  it("clamps an out-of-range number into the field's bounds on blur", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 */6 * * *"));
    const interval = screen.getByRole("spinbutton");
    await user.clear(interval);
    await user.type(interval, "24");
    // The digits arrive one at a time, so the box passes through "2" — a value
    // the user never chose. Leaving the field must land on the bound they were
    // reaching for, not on the first digit they happened to type.
    fireEvent.blur(interval);
    expect(interval).toHaveValue(23);
    expect(cronOut()).toBe("0 */23 * * *");
  });

  it("gives the weekday selection back after a trip through another day kind", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9 * * 2,4,6"));
    await user.click(screen.getByLabelText("Day pattern"));
    await user.click(await screen.findByRole("option", { name: "Every day" }));
    expect(cronOut()).toBe("0 9 * * *");
    await user.click(screen.getByLabelText("Day pattern"));
    await user.click(await screen.findByRole("option", { name: "Days of week" }));
    // Rebuilt from defaults, a glance at another kind would save the autopilot
    // to Monday — a day the user never picked.
    expect(cronOut()).toBe("0 9 * * 2,4,6");
  });

  it("warns that short months are skipped for days 29-31, and only there", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9 15 * *"));
    expect(screen.queryByText(/are skipped/)).not.toBeInTheDocument();
    const day = screen.getByLabelText("Day of month");
    await user.clear(day);
    await user.type(day, "31");
    expect(screen.getByText("Months without day 31 are skipped.")).toBeInTheDocument();
  });

  it("gives the day of month back after a trip through another day kind", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9 25 * *"));
    await user.click(screen.getByLabelText("Day pattern"));
    await user.click(await screen.findByRole("option", { name: "Every day" }));
    await user.click(screen.getByLabelText("Day pattern"));
    await user.click(await screen.findByRole("option", { name: "Day of month" }));
    expect(cronOut()).toBe("0 9 25 * *");
  });

  it("gives the window end back when the start stops overrunning it", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-15/2 * * *"));
    const startHour = screen.getByLabelText("Window start hour");
    await user.click(startHour);
    await user.keyboard("22");
    // A window cannot run backwards, so the end is dragged along…
    expect(cronOut()).toBe("0 22-22/2 * * *");
    await user.click(startHour);
    await user.keyboard("09");
    // …but it is the user's 15:00, not a value the drag invented: keeping 22
    // here would nearly double how often the autopilot runs.
    expect(cronOut()).toBe("0 9-15/2 * * *");
  });

  it("takes a window typed into the cron box as the end to hold, not the old one", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-15/2 * * *"));
    await editCronText("0 8-20/2 * * *");
    const startHour = screen.getByLabelText("Window start hour");
    await user.click(startHour);
    await user.keyboard("09");
    // The end the user just typed into the cron box is the current intent; an
    // anchor left over from before it would quietly cut the window back to 15:00.
    expect(cronOut()).toBe("0 9-20/2 * * *");
  });

  it.each(["22", "15"])(
    "keeps the window when a fixed time at or past its end is carried back (%s:00)",
    async (typed) => {
      const user = userEvent.setup();
      renderEditor(cron("0 9-15/3 * * *"));
      await user.click(screen.getByRole("button", { name: "At a time" }));
      await user.click(screen.getByLabelText("Hour"));
      await user.keyboard(typed);
      await user.click(screen.getByRole("button", { name: "At an interval" }));
      // Rebasing onto the end hour itself collapses the window just as surely as
      // rebasing past it: 15:00–15:00 is not a window.
      expect(cronOut()).toBe("0 9-15/3 * * *");
    },
  );

  it("keeps the window when a fixed time later than its end is carried back", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-15/3 * * *"));
    await user.click(screen.getByRole("button", { name: "At a time" }));
    const hour = screen.getByLabelText("Hour");
    await user.click(hour);
    await user.keyboard("22");
    await user.click(screen.getByRole("button", { name: "At an interval" }));
    // Rebasing the window onto 22:00 would drag its end up and hand back the
    // single hour 22:00–22:00 — the window the anchor exists to preserve.
    expect(cronOut()).toBe("0 9-15/3 * * *");
  });

  it("holds the dragged-open window's anchor across an interval unit switch", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-15/2 * * *"));
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("22");
    expect(cronOut()).toBe("0 22-22/2 * * *");
    // A unit switch rewrites the end from 22:00 to 22:59 without the user asking
    // for anything — it must not be read as "22 is what I want".
    await user.click(screen.getByLabelText("Interval unit"));
    await user.click(await screen.findByRole("option", { name: "minutes" }));
    await user.click(screen.getByLabelText("Interval unit"));
    await user.click(await screen.findByRole("option", { name: "hours" }));
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("09");
    expect(cronOut()).toBe("0 9-15/2 * * *");
  });

  it("takes an end typed right after a drag as the new end", async () => {
    const user = userEvent.setup();
    renderEditor(cron("0 9-15/2 * * *"));
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("22");
    // The drag already produced 22; typing 22 into the end field means the user
    // wants it — the start must not hand back the old 15 afterwards.
    await user.click(screen.getByLabelText("Window end hour"));
    await user.keyboard("22");
    await user.click(screen.getByLabelText("Window start hour"));
    await user.keyboard("09");
    expect(cronOut()).toBe("0 9-22/2 * * *");
  });

  it("gives the step back after a trip through the other interval unit", async () => {
    const user = userEvent.setup();
    renderEditor(cron("*/45 * * * *"));
    await user.click(screen.getByLabelText("Interval unit"));
    await user.click(await screen.findByRole("option", { name: "hours" }));
    // 45 hours has no meaning, so the step is clamped on the way there…
    expect(cronOut()).toBe("0 */23 * * *");
    await user.click(screen.getByLabelText("Interval unit"));
    await user.click(await screen.findByRole("option", { name: "minutes" }));
    // …but keeping the 23 on the way back would double how often it runs.
    expect(cronOut()).toBe("*/45 * * * *");
  });

  it("keeps the structured schedule under an expression it cannot represent", async () => {
    renderEditor(cron("0 17 * * 1"));
    await editCronText("0 9 1,15 * *");
    expect(screen.getByTestId("raw-out").textContent).toBe("0 9 1,15 * *");
    // The greyed controls must still show the schedule the user had, not the
    // 09:00/every-day defaults parseCron falls back to — that schedule is what
    // they return to the moment the expression is structurable again.
    expect(screen.getByRole("button", { name: "Monday", pressed: true })).toBeInTheDocument();
    expect(screen.getByLabelText("Hour")).toHaveValue("17");
  });

  it("closes the cron field when a blur brings the expression back into the model", async () => {
    renderEditor(cron("0 9 1,15 * *"));
    const input = screen.getByRole("textbox", { name: "Cron" });
    fireEvent.change(input, { target: { value: "0 9 * * *" } });
    fireEvent.blur(input);
    // The field is only open because the expression was advanced-only; once it
    // isn't, the panel goes back to its readback.
    expect(screen.getByRole("button", { name: /click to edit/ })).toBeInTheDocument();
  });

  it("keeps the cron field open and focused when Enter leaves advanced-only mode", async () => {
    renderEditor(cron("0 9 1,15 * *"));
    const input = screen.getByRole("textbox", { name: "Cron" });
    input.focus();
    fireEvent.change(input, { target: { value: "0 9 15 * *" } });
    fireEvent.keyDown(input, { key: "Enter" });
    // The expression is structurable again, so the field would collapse back to
    // its readback — under the hands of the keyboard user still typing in it.
    expect(screen.getByTestId("raw-out").textContent).toBe("null");
    expect(screen.getByRole("textbox", { name: "Cron" })).toHaveFocus();
  });

  it("marks the previous expression's next runs as pending instead of removing the row", async () => {
    renderEditor(cron("0 9 * * *"));
    const line = await waitFor(() => screen.getByText("Next runs").closest("div"));
    expect(line).not.toBeNull();

    await editCronText("0 18 * * *");
    // The description updates instantly while the preview is still the cached
    // 09:00 answer. The row stays mounted so it does not blink out on every
    // edit, but it must not read as this schedule's answer until it catches up.
    expect(screen.getByText(/At 18:00/)).toBeInTheDocument();
    expect(screen.getByText("Next runs").closest("div")).toBe(line);
    expect(line).toHaveAttribute("aria-busy", "true");

    // Once the preview catches up, the same element carries the new text.
    await waitFor(() => expect(line).toHaveAttribute("aria-busy", "false"));
    expect(screen.getByText("Next runs").closest("div")).toBe(line);
  });

  it("refreshes an expired preview per expression, not per run instant", async () => {
    previewFailure.expired = true;
    previewCalls.length = 0;
    try {
      renderEditor(cron("0 9 * * *"));
      // The first run has already passed, so the cached list is stale and the
      // editor asks again — once, then it stops: a client clock ahead of the
      // server must not turn this into a refetch on every tick.
      await waitFor(() =>
        expect(previewCalls.filter((e) => e === "TZ=UTC 0 9 * * *")).toHaveLength(2),
      );

      await editCronText("0 9 * * 1-5");
      // A different schedule that happens to share that same passed instant. A
      // guard keyed on the instant alone would count the refresh it did for the
      // previous expression as having covered this one, and leave it showing a
      // run in the past for as long as the dialog stays open.
      await waitFor(() =>
        expect(previewCalls.filter((e) => e === "TZ=UTC 0 9 * * 1-5")).toHaveLength(2),
      );
      // Still exactly one refresh each — the guard holds within an expression.
      expect(previewCalls.filter((e) => e === "TZ=UTC 0 9 * * *")).toHaveLength(2);
    } finally {
      previewFailure.expired = false;
      previewCalls.length = 0;
    }
  });

  it("blames the timezone, not the cron, when the server rejects the zone", async () => {
    previewFailure.badTimezone = true;
    try {
      renderEditor(cron("0 9 * * *"));
      await waitFor(() =>
        expect(screen.getByText(/timezone isn't recognized/)).toBeInTheDocument(),
      );
      expect(screen.queryByText("This cron expression isn't valid.")).not.toBeInTheDocument();
    } finally {
      previewFailure.badTimezone = false;
    }
  });

  // The model cannot tell an expression that is beyond the controls from one that
  // is plain wrong — both land in `raw`. Only the server can, so every notice
  // below is about what the server said, not about what the model could parse.

  it("does not call an unverified expression too advanced for the controls", async () => {
    previewFailure.transport = true;
    try {
      renderEditor(cron("0 9 1,15 * *"));
      await waitFor(() =>
        expect(screen.getByText(/server couldn't be reached/)).toBeInTheDocument(),
      );
      // The claim it must not make: with the preview endpoint down, "the server
      // takes this, the controls just can't show it" is an answer nobody gave.
      expect(screen.queryByText(/visual editor can't represent/)).not.toBeInTheDocument();
      // Saving stays open all the same — the write path validates the cron of its
      // own accord, so a preview outage must not block a schedule the server would
      // have taken.
      expect(screen.getByTestId("valid-out").textContent).toBe("true");
    } finally {
      previewFailure.transport = false;
    }
  });

  it("does not call an unreadable preview too advanced for the controls", async () => {
    previewFailure.unreadable = true;
    try {
      renderEditor(cron("0 9 1,15 * *"));
      await waitFor(() =>
        expect(screen.getByText(/server couldn't be reached/)).toBeInTheDocument(),
      );
      expect(screen.queryByText(/visual editor can't represent/)).not.toBeInTheDocument();
    } finally {
      previewFailure.unreadable = false;
    }
  });

  it("says the expression is still being checked before the server has answered", async () => {
    renderEditor(cron("0 9 1,15 * *"));
    // The round trip is a whole debounce and a request away, and an invalid cron
    // is indistinguishable from an unrepresentable one until it lands.
    expect(screen.getByText(/Checking this expression/)).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByText(/visual editor can't represent/)).toBeInTheDocument(),
    );
    expect(screen.queryByText(/Checking this expression/)).not.toBeInTheDocument();
  });

  it("calls an accepted expression advanced even when it has no upcoming runs", async () => {
    // 29 Feb is both beyond the controls (a month is pinned) and firing nowhere in
    // the preview window. An empty list is still an answer: the server took it.
    renderEditor(cron("0 0 30 2 *"));
    await waitFor(() =>
      expect(screen.getByText(/visual editor can't represent/)).toBeInTheDocument(),
    );
    expect(screen.getByText(/no upcoming runs/)).toBeInTheDocument();
  });

  it("shows the rejection, not the advanced notice, for an invalid expression", async () => {
    renderEditor(cron("0 9 * * *"));
    await editCronText("@daily");
    await waitFor(() => expect(screen.getByText(/expected exactly 5 fields/)).toBeInTheDocument());
    expect(screen.queryByText(/visual editor can't represent/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Checking this expression/)).not.toBeInTheDocument();
  });

  it("blames the timezone in advanced-only mode too", async () => {
    previewFailure.badTimezone = true;
    try {
      renderEditor(cron("0 9 1,15 * *"));
      await waitFor(() =>
        expect(screen.getByText(/timezone isn't recognized/)).toBeInTheDocument(),
      );
      expect(screen.queryByText(/visual editor can't represent/)).not.toBeInTheDocument();
    } finally {
      previewFailure.badTimezone = false;
    }
  });

  it("surfaces a rejection whose code it does not know, with the server's reason", async () => {
    previewFailure.unknownCode = true;
    try {
      renderEditor(cron("0 9 * * *"));
      // An unclassifiable 400 is still a refusal: it blocks the save and carries
      // the server's own words, rather than being swallowed as an outage.
      await waitFor(() => expect(screen.getByText(/rejected by policy/)).toBeInTheDocument());
      expect(screen.getByTestId("valid-out").textContent).toBe("false");
      expect(screen.getByText("This cron expression isn't valid.")).toBeInTheDocument();
    } finally {
      previewFailure.unknownCode = false;
    }
  });

  // Every numeric field in the panel is a wheel, not a slider: stepping off one
  // end of its range comes out at the other. A field that stopped dead at its
  // bound would leave a keyboard user pressing a key that does nothing, and make
  // 23:00 → 00:00 a mouse-only move.
  describe("keyboard stepping wraps within each field's range", () => {
    it("wraps the fixed time's hour and minute", async () => {
      const user = userEvent.setup();
      renderEditor(cron("0 23 * * *"));
      await user.click(screen.getByLabelText("Hour"));
      await user.keyboard("{ArrowUp}");
      expect(cronOut()).toBe("0 0 * * *");
      await user.keyboard("{ArrowDown}");
      expect(cronOut()).toBe("0 23 * * *");

      await user.click(screen.getByLabelText("Minute"));
      await user.keyboard("{ArrowDown}");
      expect(cronOut()).toBe("59 23 * * *");
      await user.keyboard("{ArrowUp}");
      expect(cronOut()).toBe("0 23 * * *");
    });

    it("wraps the firing minute an all-day interval carries in its window start", async () => {
      const user = userEvent.setup();
      renderEditor(cron("0 */2 * * *"));
      await user.click(screen.getByLabelText("Window start minute"));
      await user.keyboard("{ArrowDown}");
      // Still all day — only the minute moved, so the schedule keeps its "*/2".
      expect(cronOut()).toBe("59 */2 * * *");
    });

    it("wraps the interval step within the unit's range", async () => {
      const user = userEvent.setup();
      // The step starts at 1, which is its minimum — not 0, so its wrap-around
      // lands on the unit's max rather than on some shared 0.
      renderEditor(cron("0 * * * *"));
      await user.click(screen.getByLabelText("Interval"));
      await user.keyboard("{ArrowDown}");
      expect(cronOut()).toBe("0 */23 * * *");
      await user.keyboard("{ArrowUp}");
      expect(cronOut()).toBe("0 * * * *");

      // Minutes carry a different ceiling, and the wrap must follow it.
      await user.click(screen.getByLabelText("Interval unit"));
      await user.click(await screen.findByRole("option", { name: "minutes" }));
      await user.click(screen.getByLabelText("Interval"));
      await user.keyboard("{ArrowDown}");
      expect(cronOut()).toBe("*/59 * * * *");
    });

    it("wraps the day of month", async () => {
      const user = userEvent.setup();
      renderEditor(cron("0 9 31 * *"));
      const day = screen.getByLabelText("Day of month");
      await user.click(day);
      await user.keyboard("{ArrowUp}");
      expect(cronOut()).toBe("0 9 1 * *");
      await user.keyboard("{ArrowDown}");
      expect(cronOut()).toBe("0 9 31 * *");
    });

    it("wraps the window end within the range its start leaves it", async () => {
      const user = userEvent.setup();
      renderEditor(cron("0 9-15/2 * * *"));
      const end = screen.getByLabelText("Window end hour");
      await user.click(end);
      // Pull the end down onto the start, so the next step is off the bottom of
      // its range. (Typed, not parsed: "9-9/2" reads back as a fixed 9:00.)
      await user.keyboard("09");
      expect(cronOut()).toBe("0 9-9/2 * * *");
      // A completed pair hands focus on to the minute; the hour is where the
      // stepping is being tested.
      await user.click(end);
      // The end can never precede the start, so its range is 9–23: stepping
      // below 9 comes out at 23, not at 8 (which has no cron form) and not stuck
      // on 9 (which is the same key doing nothing).
      await user.keyboard("{ArrowDown}");
      expect(cronOut()).toBe("0 9-23/2 * * *");
      await user.keyboard("{ArrowUp}");
      expect(cronOut()).toBe("0 9-9/2 * * *");
    });

    it("keeps an out-of-range end unreachable by typing, too", async () => {
      const user = userEvent.setup();
      renderEditor(cron("0 9-15/2 * * *"));
      await user.click(screen.getByLabelText("Window end hour"));
      // 08 is before the start: the field holds it at the start hour rather than
      // wrapping a half-typed number round to 23.
      await user.keyboard("08");
      expect(cronOut()).toBe("0 9-9/2 * * *");
    });

    it("still types a two-digit end whose first digit alone is out of range", async () => {
      const user = userEvent.setup();
      renderEditor(cron("0 9-15/2 * * *"));
      await user.click(screen.getByLabelText("Window end hour"));
      // The "1" of 18 is below the 9 the field starts at. Clamping it for display
      // must not overwrite the digit being typed — the pair still has to land as 18.
      await user.keyboard("18");
      expect(cronOut()).toBe("0 9-18/2 * * *");
    });
  });

  describe("timezone prefix extraction", () => {
    // Extraction rewrites two controls the user is looking at — the zone lands
    // in the picker, the cron box loses the prefix — so a TYPED prefix waits
    // for the server: the text commits verbatim first, and only the preview's
    // 200 for that exact text promotes it to the extracted pair.
    it("moves a typed CRON_TZ= prefix into the picker once the server accepts it", async () => {
      renderEditor(cron("0 9 * * *"));
      await editCronText("CRON_TZ=Asia/Tokyo 30 9 * * 1-5");
      // Before the verdict: verbatim, both controls untouched.
      expect(screen.getByTestId("raw-out").textContent).toBe("CRON_TZ=Asia/Tokyo 30 9 * * 1-5");
      expect(screen.getByTestId("tz-out").textContent).toBe("UTC");
      // The 200 lands both rewrites at once.
      await waitFor(() =>
        expect(screen.getByTestId("tz-out").textContent).toBe("Asia/Tokyo"),
      );
      expect(screen.getByTestId("raw-out").textContent).toBe("null");
      expect(cronOut()).toBe("30 9 * * 1-5");
    });

    it("promotes into advanced-only when the rest is beyond the model", async () => {
      renderEditor(cron("0 9 * * *"));
      await editCronText("TZ=Asia/Tokyo ?/2 ?/2 * * ?/2");
      await waitFor(() =>
        expect(screen.getByTestId("tz-out").textContent).toBe("Asia/Tokyo"),
      );
      expect(screen.getByTestId("raw-out").textContent).toBe("?/2 ?/2 * * ?/2");
    });

    it("never promotes an expression the server rejected", async () => {
      renderEditor(cron("0 9 * * *"));
      await editCronText("TZ=Europe/Berlin 0 9 * * *");
      // The mock's server knows no Berlin; the browser does. The text must
      // stay exactly as typed, under the server's own error — the picker never
      // shows a zone the server refused.
      await waitFor(() =>
        expect(screen.getByText("This cron expression isn't valid.")).toBeInTheDocument(),
      );
      expect(screen.getByTestId("raw-out").textContent).toBe("TZ=Europe/Berlin 0 9 * * *");
      expect(screen.getByTestId("tz-out").textContent).toBe("UTC");
    });

    it("leaves a zone the picker cannot offer buried in the text, even accepted", async () => {
      renderEditor(cron("0 9 * * *"));
      await editCronText("TZ=Local 0 9 * * *");
      // "Local" is legal server-side (the SERVER host's zone) — the preview
      // 200s — but no browser zone: there is nothing the picker could show for
      // it, so the expression stays verbatim and keeps meaning what it meant.
      await waitFor(() => expect(screen.getByText("Next runs")).toBeInTheDocument());
      expect(screen.getByTestId("raw-out").textContent).toBe("TZ=Local 0 9 * * *");
      expect(screen.getByTestId("tz-out").textContent).toBe("UTC");
    });

    it("leaves a prefix with no schedule after it verbatim, timezone untouched", async () => {
      renderEditor(cron("0 9 * * *"));
      await editCronText("TZ=Asia/Tokyo");
      // robfig v3.0.1 panics on this shape and the (guarded) server rejects it;
      // rewriting any of it would put words in the user's mouth mid-typo.
      await waitFor(() =>
        expect(screen.getByText("This cron expression isn't valid.")).toBeInTheDocument(),
      );
      expect(screen.getByTestId("raw-out").textContent).toBe("TZ=Asia/Tokyo");
      expect(screen.getByTestId("tz-out").textContent).toBe("UTC");
    });

    it("promotes a lowercase zone under its canonical spelling", async () => {
      renderEditor(cron("0 9 * * *"));
      await editCronText("TZ=asia/shanghai 0 9 * * *");
      // The mock's server takes the lowercase spelling (a case-insensitive
      // filesystem does), but the picker's list knows only "Asia/Shanghai" —
      // seating "asia/shanghai" would show the same zone twice.
      await waitFor(() =>
        expect(screen.getByTestId("tz-out").textContent).toBe("Asia/Shanghai"),
      );
      expect(screen.getByTestId("raw-out").textContent).toBe("null");
      expect(cronOut()).toBe("0 9 * * *");
    });

    it("extracts immediately on hydration — a stored expression already passed the server", () => {
      renderEditor(cron("CRON_TZ=Asia/Tokyo 30 9 * * 1-5"));
      expect(screen.getByTestId("raw-out").textContent).toBe("null");
      expect(screen.getByTestId("tz-out").textContent).toBe("Asia/Tokyo");
      expect(cronOut()).toBe("30 9 * * 1-5");
    });
  });

  describe("timezone prefix as the wire form", () => {
    // The expression that leaves the editor — for the preview and for saving —
    // always carries the timezone as a TZ= prefix, so the cron string and the
    // timezone column can never disagree about which zone governs the schedule.
    it("serialises the picker's zone into the expression it validates and saves", async () => {
      renderEditor(cron("0 9 * * *"));
      expect(wireOut()).toBe("TZ=UTC 0 9 * * *");
      previewCalls.length = 0;
      fireEvent.change(screen.getByTestId("timezone-picker"), {
        target: { value: "Asia/Shanghai" },
      });
      // The prefix follows the picker; the fields do not move.
      expect(wireOut()).toBe("TZ=Asia/Shanghai 0 9 * * *");
      expect(cronOut()).toBe("0 9 * * *");
      // And the preview judges the prefixed pair, not the bare fields.
      await waitFor(() =>
        expect(previewCalls).toContain("TZ=Asia/Shanghai 0 9 * * *"),
      );
    });

    it("draws the prefix as a fixed segment, outside the editable text", async () => {
      renderEditor(cron("0 9 * * *"));
      const input = await openCronInput();
      // The zone is not characters in the field — it cannot be half-deleted
      // into the no-space shape robfig panics on.
      expect((input as HTMLInputElement).value).toBe("0 9 * * *");
      expect(screen.getByText("TZ=UTC")).toBeInTheDocument();
    });

    it("keeps focus in the field when the fixed segment is clicked", async () => {
      renderEditor(cron("0 9 * * *"));
      await openCronInput();
      // The browser moves focus on mousedown, before the addon's click-to-focus
      // can run — and blurring the input is what closes the field. The segment
      // must swallow the mousedown (fireEvent returns false when the default
      // was prevented) so focus never leaves.
      expect(fireEvent.mouseDown(screen.getByText("TZ=UTC"))).toBe(false);
    });

    it("yields the fixed segment while a typed prefix awaits the verdict", async () => {
      renderEditor(cron("0 9 * * *"));
      await editCronText("TZ=Local 0 9 * * *");
      // The committed text carries its own prefix; drawing the picker's next
      // to it would read as an expression carrying both. It also must not be
      // stacked onto the wire form — robfig strips ONE prefix and would read
      // the second as a field.
      await waitFor(() => expect(screen.getByText("Next runs")).toBeInTheDocument());
      expect(wireOut()).toBe("TZ=Local 0 9 * * *");
      const input = await openCronInput();
      expect((input as HTMLInputElement).value).toBe("TZ=Local 0 9 * * *");
      expect(screen.queryByText("TZ=UTC")).not.toBeInTheDocument();
    });

    it("re-serialises a promoted CRON_TZ= under the canonical TZ= segment", async () => {
      renderEditor(cron("0 9 * * *"));
      await editCronText("CRON_TZ=Asia/Tokyo 30 9 * * 1-5");
      await waitFor(() =>
        expect(screen.getByTestId("tz-out").textContent).toBe("Asia/Tokyo"),
      );
      expect(wireOut()).toBe("TZ=Asia/Tokyo 30 9 * * 1-5");
    });
  });
});
