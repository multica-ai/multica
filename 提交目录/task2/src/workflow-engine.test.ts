import { describe, expect, it } from "vitest";
import { WorkflowEngine } from "./workflow-engine";

function ids(engine: WorkflowEngine): string[] {
  return engine.snapshot().map((task) => `${task.id}:${task.status}`);
}

describe("WorkflowEngine", () => {
  it("runs a linear chain in order", () => {
    const engine = new WorkflowEngine(2);
    engine.addTask({ id: "A" });
    engine.addTask({ id: "B" });
    engine.addTask({ id: "C" });
    engine.addDependency("B", "A");
    engine.addDependency("C", "B");

    expect(engine.claimNextTasks()).toEqual(["A"]);
    engine.completeTask("A");
    expect(engine.claimNextTasks()).toEqual(["B"]);
    engine.completeTask("B");
    expect(engine.claimNextTasks()).toEqual(["C"]);
    engine.completeTask("C");

    expect(ids(engine)).toEqual(["A:completed", "B:completed", "C:completed"]);
  });

  it("releases both branches in a diamond before the join", () => {
    const engine = new WorkflowEngine(3);
    engine.addTask({ id: "A" });
    engine.addTask({ id: "B" });
    engine.addTask({ id: "C" });
    engine.addTask({ id: "D" });
    engine.addDependency("B", "A");
    engine.addDependency("C", "A");
    engine.addDependency("D", "B");
    engine.addDependency("D", "C");

    expect(engine.claimNextTasks()).toEqual(["A"]);
    engine.completeTask("A");

    const next = engine.claimNextTasks();
    expect(next).toEqual(["B", "C"]);
    engine.completeTask("B");
    expect(engine.claimNextTasks()).toEqual([]);
    engine.completeTask("C");
    expect(engine.claimNextTasks()).toEqual(["D"]);
  });

  it("detects cycles when adding a dependency", () => {
    const engine = new WorkflowEngine(1);
    engine.addTask({ id: "A" });
    engine.addTask({ id: "B" });
    engine.addTask({ id: "C" });
    engine.addDependency("B", "A");
    engine.addDependency("C", "B");

    expect(() => engine.addDependency("A", "C")).toThrow(/Cycle detected/);
  });

  it("keeps unrelated branches moving when skip is used", () => {
    const engine = new WorkflowEngine(3);
    engine.addTask({ id: "A", failureStrategy: "skip" });
    engine.addTask({ id: "B" });
    engine.addTask({ id: "C" });
    engine.addTask({ id: "D" });
    engine.addTask({ id: "E" });
    engine.addDependency("B", "A");
    engine.addDependency("D", "C");
    engine.addDependency("E", "D");

    expect(engine.claimNextTasks()).toEqual(["A", "C"]);
    engine.failTask("A");
    engine.completeTask("C");

    expect(ids(engine)).toContain("B:blocked");
    expect(engine.claimNextTasks()).toEqual(["D"]);
  });

  it("retries before escalating to fail-fast", () => {
    const engine = new WorkflowEngine(1);
    engine.addTask({ id: "A", failureStrategy: "retry", maxRetries: 1 });
    engine.addTask({ id: "B" });
    engine.addDependency("B", "A");

    expect(engine.claimNextTasks()).toEqual(["A"]);
    expect(engine.failTask("A")).toEqual({ outcome: "retry" });
    expect(ids(engine)).toContain("A:pending");

    expect(engine.claimNextTasks()).toEqual(["A"]);
    expect(engine.failTask("A")).toEqual({ outcome: "fail_fast" });
    expect(ids(engine)).toContain("B:cancelled");
  });

  it("caps parallel claims at the concurrency limit", () => {
    const engine = new WorkflowEngine(3);
    for (let i = 1; i <= 10; i += 1) {
      engine.addTask({ id: `T${i}` });
    }

    expect(engine.claimNextTasks()).toEqual(["T1", "T2", "T3"]);
    engine.completeTask("T1");
    expect(engine.claimNextTasks()).toEqual(["T4"]);
    engine.completeTask("T2");
    engine.completeTask("T3");
    expect(engine.claimNextTasks().length).toBe(2);
  });
});

