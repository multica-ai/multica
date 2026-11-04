import { action, makeObservable, observable } from "mobx";
import "../mobx-config";

export type WorkspaceLoadStatus = "idle" | "loading" | "ready" | "failed";

/** Whether a workspace's conversation list has been read, for what the list shows meanwhile. */
export class WorkspaceLoadModel {
  status: WorkspaceLoadStatus = "idle";
  errorMessage: string | null = null;

  constructor() {
    makeObservable(this, {
      status: observable,
      errorMessage: observable,
      begin: action,
      succeed: action,
      fail: action,
    });
  }

  begin(): void {
    this.status = "loading";
    this.errorMessage = null;
  }

  succeed(): void {
    this.status = "ready";
    this.errorMessage = null;
  }

  fail(message: string | null): void {
    this.status = "failed";
    this.errorMessage = message;
  }
}
