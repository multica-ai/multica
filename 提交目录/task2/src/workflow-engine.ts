import type { FailureStrategy, TaskDefinition, TaskId, TaskSnapshot, TaskStatus } from "./types";

interface TaskRecord {
  id: TaskId;
  dependsOn: Set<TaskId>;
  dependents: Set<TaskId>;
  status: TaskStatus;
  failureStrategy: FailureStrategy;
  retriesRemaining: number;
  order: number;
}

export class WorkflowEngine {
  private readonly tasks = new Map<TaskId, TaskRecord>();
  private nextOrder = 0;

  constructor(private readonly concurrencyLimit: number) {
    if (!Number.isInteger(concurrencyLimit) || concurrencyLimit <= 0) {
      throw new Error("concurrencyLimit must be a positive integer");
    }
  }

  addTask(definition: TaskDefinition): void {
    if (this.tasks.has(definition.id)) {
      throw new Error(`Task already exists: ${definition.id}`);
    }

    const retryBudget = Math.max(0, definition.maxRetries ?? 1);
    this.tasks.set(definition.id, {
      id: definition.id,
      dependsOn: new Set(),
      dependents: new Set(),
      status: "pending",
      failureStrategy: definition.failureStrategy ?? "fail_fast",
      retriesRemaining: retryBudget,
      order: this.nextOrder++,
    });

    const linkedDependencies: TaskId[] = [];
    for (const dependsOnId of definition.dependsOn ?? []) {
      try {
        this.addDependency(definition.id, dependsOnId);
        linkedDependencies.push(dependsOnId);
      } catch (error) {
        for (const linkedDependency of linkedDependencies) {
          this.removeDependency(definition.id, linkedDependency);
        }
        this.tasks.delete(definition.id);
        throw error;
      }
    }
  }

  addDependency(taskId: TaskId, dependsOnId: TaskId): void {
    const task = this.requireTask(taskId);
    const parent = this.requireTask(dependsOnId);

    if (taskId === dependsOnId) {
      throw new Error(`Cycle detected: ${taskId} cannot depend on itself`);
    }

    task.dependsOn.add(dependsOnId);
    parent.dependents.add(taskId);

    if (this.hasPath(taskId, dependsOnId)) {
      this.removeDependency(taskId, dependsOnId);
      throw new Error(`Cycle detected when linking ${dependsOnId} -> ${taskId}`);
    }
  }

  claimNextTasks(limit = this.availableSlots()): TaskId[] {
    if (limit <= 0) return [];

    const ready = this.getDispatchableTasks()
      .slice(0, limit);

    for (const task of ready) {
      task.status = "running";
    }

    return ready.map((task) => task.id);
  }

  completeTask(taskId: TaskId): void {
    const task = this.requireTask(taskId);
    this.assertStatus(task, "running");
    task.status = "completed";
  }

  failTask(taskId: TaskId): { outcome: "retry" | "skip" | "fail_fast" } {
    const task = this.requireTask(taskId);
    this.assertStatus(task, "running");

    if (task.failureStrategy === "retry" && task.retriesRemaining > 0) {
      task.retriesRemaining -= 1;
      task.status = "pending";
      return { outcome: "retry" };
    }

    task.status = "failed";

    if (task.failureStrategy === "skip") {
      this.blockDescendants(taskId);
      return { outcome: "skip" };
    }

    this.cancelAllUnfinished();
    return { outcome: "fail_fast" };
  }

  cancelTask(taskId: TaskId): void {
    const task = this.requireTask(taskId);
    if (task.status === "completed" || task.status === "cancelled" || task.status === "blocked") {
      return;
    }
    task.status = "cancelled";
  }

  snapshot(): TaskSnapshot[] {
    return [...this.tasks.values()]
      .sort((a, b) => a.order - b.order)
      .map((task) => ({
        id: task.id,
        dependsOn: [...task.dependsOn],
        dependents: [...task.dependents],
        status: task.status,
        failureStrategy: task.failureStrategy,
        retriesRemaining: task.retriesRemaining,
      }));
  }

  private availableSlots(): number {
    const running = [...this.tasks.values()].filter((task) => task.status === "running").length;
    return Math.max(0, this.concurrencyLimit - running);
  }

  private getDispatchableTasks(): TaskRecord[] {
    return [...this.tasks.values()]
      .filter((task) => task.status === "pending")
      .filter((task) => this.areDependenciesSatisfied(task))
      .sort((a, b) => a.order - b.order);
  }

  private areDependenciesSatisfied(task: TaskRecord): boolean {
    for (const depId of task.dependsOn) {
      const dep = this.requireTask(depId);
      if (dep.status !== "completed") {
        return false;
      }
    }
    return true;
  }

  private blockDescendants(sourceId: TaskId): void {
    const source = this.requireTask(sourceId);
    for (const childId of source.dependents) {
      const child = this.requireTask(childId);
      if (child.status === "completed" || child.status === "failed" || child.status === "cancelled" || child.status === "blocked") {
        continue;
      }
      child.status = "blocked";
      this.blockDescendants(childId);
    }
  }

  private cancelAllUnfinished(): void {
    for (const task of this.tasks.values()) {
      if (task.status === "completed" || task.status === "failed" || task.status === "cancelled" || task.status === "blocked") {
        continue;
      }
      task.status = "cancelled";
    }
  }

  private removeDependency(taskId: TaskId, dependsOnId: TaskId): void {
    const task = this.tasks.get(taskId);
    const parent = this.tasks.get(dependsOnId);
    task?.dependsOn.delete(dependsOnId);
    parent?.dependents.delete(taskId);
  }

  private hasPath(fromId: TaskId, targetId: TaskId): boolean {
    const stack: TaskId[] = [fromId];
    const visited = new Set<TaskId>();

    while (stack.length > 0) {
      const currentId = stack.pop() as TaskId;
      if (currentId === targetId) {
        return true;
      }
      if (visited.has(currentId)) {
        continue;
      }
      visited.add(currentId);
      const current = this.tasks.get(currentId);
      if (!current) continue;
      for (const nextId of current.dependents) {
        stack.push(nextId);
      }
    }

    return false;
  }

  private requireTask(taskId: TaskId): TaskRecord {
    const task = this.tasks.get(taskId);
    if (!task) {
      throw new Error(`Unknown task: ${taskId}`);
    }
    return task;
  }

  private assertStatus(task: TaskRecord, expected: TaskStatus): void {
    if (task.status !== expected) {
      throw new Error(`Task ${task.id} must be ${expected}, got ${task.status}`);
    }
  }
}
