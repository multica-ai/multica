import { describe, expect, it } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  getNoteMentionScope,
  noteMentionScopeKey,
} from "./note-mention-scope";

const WS = "ws-1";
const NOTE = "note-1";

function qcWithScope(noAccess: string[]): QueryClient {
  const qc = new QueryClient();
  qc.setQueryData(noteMentionScopeKey(WS, NOTE), { noAccess });
  return qc;
}

describe("getNoteMentionScope", () => {
  it("is inactive when there is no note in context", () => {
    const scope = getNoteMentionScope(new QueryClient(), WS, null);
    expect(scope.active).toBe(false);
    // Fails open — everyone is allowed when scoping does not apply.
    expect(scope.allows("anyone")).toBe(true);
  });

  it("is inactive when the answer has not loaded yet", () => {
    const scope = getNoteMentionScope(new QueryClient(), WS, NOTE);
    expect(scope.active).toBe(false);
    expect(scope.allows("anyone")).toBe(true);
  });

  it("allows members not in the no-access set", () => {
    const scope = getNoteMentionScope(qcWithScope(["blocked-user"]), WS, NOTE);
    expect(scope.active).toBe(true);
    expect(scope.allows("allowed-user")).toBe(true);
  });

  it("blocks members the server reported as no-access", () => {
    const scope = getNoteMentionScope(qcWithScope(["blocked-user"]), WS, NOTE);
    expect(scope.allows("blocked-user")).toBe(false);
  });

  it("keeps ownerless agents (null owner) even when scoped", () => {
    const scope = getNoteMentionScope(qcWithScope(["blocked-user"]), WS, NOTE);
    // Agents pass their owner_id; a workspace-wide agent has none.
    expect(scope.allows(null)).toBe(true);
    expect(scope.allows(undefined)).toBe(true);
  });

  it("scopes an agent by its owner's access", () => {
    const scope = getNoteMentionScope(qcWithScope(["blocked-owner"]), WS, NOTE);
    // Agent owned by someone without note access is hidden…
    expect(scope.allows("blocked-owner")).toBe(false);
    // …but an agent owned by someone with access is shown.
    expect(scope.allows("allowed-owner")).toBe(true);
  });
});
