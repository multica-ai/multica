// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { Agent } from "../types";
import { DEFAULT_OFFICE_ROOM, isOfficeRoom, officeRoomOf } from "./office-room";

describe("officeRoomOf", () => {
  it("returns the room the agent sits in", () => {
    expect(officeRoomOf({ office_room: "planning" } as Agent)).toBe("planning");
  });

  it("falls back to the default room when the field is missing or unknown", () => {
    expect(officeRoomOf({} as Agent)).toBe(DEFAULT_OFFICE_ROOM);
    expect(officeRoomOf({ office_room: "sales" } as unknown as Agent)).toBe(DEFAULT_OFFICE_ROOM);
    expect(officeRoomOf({ office_room: "" } as unknown as Agent)).toBe(DEFAULT_OFFICE_ROOM);
  });
});

describe("isOfficeRoom", () => {
  it("accepts only the four room keys", () => {
    for (const room of ["product", "research", "planning", "general"]) {
      expect(isOfficeRoom(room)).toBe(true);
    }
    for (const value of ["Planning", "sales", "", null, undefined, 3]) {
      expect(isOfficeRoom(value)).toBe(false);
    }
  });
});
