import { describe, expect, it } from "vitest";
import { detectInstruction, type NamedActor } from "./detect-instruction";

const lambda: NamedActor = { type: "agent", id: "agent-lambda", name: "Lambda" };
const lambdaPro: NamedActor = { type: "agent", id: "agent-lambda-pro", name: "Lambda Pro" };
const backend: NamedActor = { type: "squad", id: "squad-backend", name: "Backend" };
const actors = [lambda, lambdaPro, backend];

const settled = { hasResults: true, resultsSettled: true };
const empty = { hasResults: false, resultsSettled: true };

describe("detectInstruction", () => {
  it("reads a sentence naming an agent as an instruction to it", () => {
    expect(detectInstruction("让 Lambda 修一下 Safari 登录回调的 500", actors, settled)).toEqual({
      reason: "names_actor",
      actor: lambda,
    });
    expect(detectInstruction("ask the backend squad to rotate keys", actors, settled)?.actor).toEqual(backend);
  });

  it("prefers the longest matching name", () => {
    expect(detectInstruction("Lambda Pro please draft the changelog", actors, settled)?.actor).toEqual(lambdaPro);
  });

  it("treats a bare name, or a name plus one word, as a search", () => {
    expect(detectInstruction("lambda", actors, settled)).toBeNull();
    expect(detectInstruction("lambda runs", actors, settled)).toBeNull();
  });

  it("only matches Latin names as whole words", () => {
    expect(detectInstruction("lambdas and closures", actors, settled)).toBeNull();
  });

  it("reads an imperative opener as an instruction without an actor", () => {
    expect(detectInstruction("fix the safari login callback", actors, settled)).toEqual({
      reason: "starts_with_verb",
      actor: null,
    });
    expect(detectInstruction("修复登录回调", actors, settled)?.reason).toBe("starts_with_verb");
    expect(detectInstruction("fix", actors, settled)).toBeNull();
  });

  it("falls back on multi-word input only once the search came back empty", () => {
    expect(detectInstruction("safari callback returns 500", actors, empty)?.reason).toBe("no_results");
    expect(detectInstruction("safari callback returns 500", actors, settled)).toBeNull();
    expect(
      detectInstruction("safari callback returns 500", actors, { hasResults: false, resultsSettled: false }),
    ).toBeNull();
    expect(detectInstruction("safari", actors, empty)).toBeNull();
  });

  it("ignores very short input", () => {
    expect(detectInstruction("修复 a", actors, empty)).toBeNull();
  });
});
