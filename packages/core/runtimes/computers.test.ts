// @vitest-environment node
import { describe, expect, it } from "vitest";
import { countRuntimeComputers } from "./computers";

describe("countRuntimeComputers", () => {
  it("counts six CLIs on one daemon as one computer", () => {
    expect(countRuntimeComputers(Array.from({ length: 6 }, (_, i) => ({
      id: `rt-${i}`, daemon_id: "daemon-1",
    })))).toBe(1);
  });

  it("counts distinct daemons and each daemon-less runtime separately", () => {
    expect(countRuntimeComputers([
      { id: "rt-1", daemon_id: "daemon-1" },
      { id: "rt-2", daemon_id: "daemon-2" },
      { id: "rt-3", daemon_id: "daemon-2" },
      { id: "daemon-1", daemon_id: null },
      { id: "rt-4", daemon_id: null },
    ])).toBe(4);
    expect(countRuntimeComputers([])).toBe(0);
  });
});
