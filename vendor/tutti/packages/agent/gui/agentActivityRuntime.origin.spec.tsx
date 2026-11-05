import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import {
  ConversationServicesProvider,
  resetConversationServicesForTests,
  useConversationServices,
  type ConversationServices
} from "./conversationServices";

function createRuntime(origin: string): ConversationServices {
  return { origin } as unknown as ConversationServices;
}

function RuntimeIdentity({ testId }: { testId: string }) {
  const runtime = useConversationServices();
  return <div data-testid={testId}>{runtime.origin}</div>;
}

afterEach(() => {
  cleanup();
  resetConversationServicesForTests();
});

describe("ConversationServicesProvider identity isolation", () => {
  it("resolves coexisting runtimes only from the nearest provider", () => {
    render(
      <>
        <ConversationServicesProvider runtime={createRuntime("origin-local")}>
          <RuntimeIdentity testId="local-runtime" />
        </ConversationServicesProvider>
        <ConversationServicesProvider runtime={createRuntime("origin-shared")}>
          <RuntimeIdentity testId="shared-runtime" />
        </ConversationServicesProvider>
      </>
    );

    expect(screen.getByTestId("local-runtime")).toHaveTextContent(
      "origin-local"
    );
    expect(screen.getByTestId("shared-runtime")).toHaveTextContent(
      "origin-shared"
    );
  });

  it("does not let a later sibling provider replace an existing consumer", () => {
    const view = render(
      <ConversationServicesProvider runtime={createRuntime("origin-local")}>
        <RuntimeIdentity testId="local-runtime" />
      </ConversationServicesProvider>
    );

    view.rerender(
      <>
        <ConversationServicesProvider runtime={createRuntime("origin-local")}>
          <RuntimeIdentity testId="local-runtime" />
        </ConversationServicesProvider>
        <ConversationServicesProvider runtime={createRuntime("origin-shared")}>
          <RuntimeIdentity testId="shared-runtime" />
        </ConversationServicesProvider>
      </>
    );

    expect(screen.getByTestId("local-runtime")).toHaveTextContent(
      "origin-local"
    );
  });
});
