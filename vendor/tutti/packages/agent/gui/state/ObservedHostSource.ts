import { createAtom } from "mobx";

/** Subscribes only while a computed value or observer consumes this host source. */
export class ObservedHostSource<T> {
  private unsubscribe: (() => void) | undefined;
  private readonly atom = createAtom(
    "conversation.hostSource",
    () => {
      this.unsubscribe = this.subscribe(() => this.atom.reportChanged());
    },
    () => {
      this.unsubscribe?.();
      this.unsubscribe = undefined;
    }
  );

  constructor(
    private readonly read: () => T,
    private readonly subscribe: (listener: () => void) => () => void
  ) {}

  get value(): T {
    this.atom.reportObserved();
    return this.read();
  }
}
