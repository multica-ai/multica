import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import "@fontsource-variable/geist-mono";
import "@fontsource-variable/inter";
import "@fontsource-variable/inter/wght-italic.css";
import "@fontsource-variable/source-serif-4";
import "@fontsource-variable/source-serif-4/wght-italic.css";
import { AgentGUIStartupShell } from "@tutti-os/agent-gui/startup-shell";
import { Button } from "@tutti-os/ui-system/components";
import {
  Component,
  Profiler,
  useEffect,
  useRef,
  useState,
  type ErrorInfo,
  type ReactNode
} from "react";
import {
  AGENT_GUI_ROOT_CLASS,
  applyAgentGuiAppearance,
  syncAgentGuiDocument
} from "./agent-gui-appearance";
import "./agent-gui.generated.css";
import { AgentGuiApplication } from "./application";
import type {
  AgentGuiHostServices,
  AgentGuiInitSnapshot,
  LocalConversationSource
} from "./protocol";
import { createMulticaAgentGuiRuntime } from "./runtime";
import { installConversationUiProfile } from "./ui-performance";
import { observer } from "mobx-react-lite";
import { observer as observeClass } from "mobx-react";
const ErrorView = observer(ErrorViewRender);
export const AgentGuiMount = observer(AgentGuiMountRender);

function ErrorViewRender({
  error,
  chinese,
  onRetry
}: {
  error: Error;
  chinese: boolean;
  onRetry(): void;
}) {
  return (
    <div
      role="alert"
      className="flex h-full flex-col items-center justify-center gap-3 p-6 text-sm text-[var(--text-primary)]"
    >
      <p>{chinese ? "无法加载智能体界面" : "Could not load Agent GUI"}</p>
      <details className="max-w-full overflow-auto text-[var(--text-secondary)]">
        <summary>{chinese ? "错误详情" : "Error details"}</summary>
        <pre className="whitespace-pre-wrap">{error.message}</pre>
      </details>
      <Button onClick={onRetry}>{chinese ? "重试" : "Retry"}</Button>
    </div>
  );
}

class AgentGuiErrorBoundary extends Component<
  { chinese: boolean; onRetry(): void; children: ReactNode },
  { error: Error | null }
> {
  state = { error: null as Error | null };
  static getDerivedStateFromError(error: Error) {
    return { error };
  }
  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error("Agent GUI rendering failed", error, info.componentStack);
  }
  render() {
    return this.state.error ? (
      <ErrorView
        error={this.state.error}
        chinese={this.props.chinese}
        onRetry={this.props.onRetry}
      />
    ) : (
      this.props.children
    );
  }
}
observeClass(AgentGuiErrorBoundary);

const Rendered = observer(function Rendered({
  onRendered,
  children
}: {
  onRendered?: () => void;
  children: ReactNode;
}) {
  useEffect(() => {
    onRendered?.();
  }, [onRendered]);
  return children;
});

type Handle = ReturnType<typeof createMulticaAgentGuiRuntime>;

export interface AgentGuiMountProps {
  client: AgentGuiHostServices;
  initial: AgentGuiInitSnapshot;
  localConversations: LocalConversationSource;
  onRendered?: () => void;
}

/** One GUI instance. Its runtime lives exactly as long as this component. */
function AgentGuiMountRender({
  client,
  initial,
  localConversations,
  onRendered
}: AgentGuiMountProps) {
  const [root, setRoot] = useState<HTMLDivElement | null>(null);
  const [state, setState] = useState<{
    handle: Handle | null;
    error: Error | null;
  }>({ handle: null, error: null });
  const [attempt, setAttempt] = useState(0);
  const chinese = initial.locale === "zh-CN";

  useEffect(() => {
    let current = true;
    const handle = createMulticaAgentGuiRuntime(
      client,
      initial,
      localConversations
    );
    setState({ handle: null, error: null });
    handle.initialize().then(
      () => {
        if (current) setState({ handle, error: null });
      },
      (error: unknown) => {
        if (current)
          setState({
            handle: null,
            error: error instanceof Error ? error : new Error(String(error))
          });
      }
    );
    return () => {
      current = false;
      handle.dispose();
    };
    // `initial` only seeds a runtime; later snapshots arrive as init events.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, attempt]);

  useEffect(() => {
    syncAgentGuiDocument(initial);
  }, [initial]);

  const retry = () => setAttempt((value) => value + 1);
  const retryCallback = usePersistCallback(retry);
  const refProp1 = usePersistCallback<
    Extract<
      NonNullable<React.ComponentProps<"div">["ref"]>,
      (...args: never[]) => unknown
    >
  >((element) => {
    setRoot(element);
    if (element && !element.dataset.theme)
      applyAgentGuiAppearance(element, initial);
  });
  return (
    <div
      ref={refProp1}
      className={AGENT_GUI_ROOT_CLASS}
      data-testid="agent-gui-root"
    >
      {state.error ? (
        <Rendered onRendered={onRendered}>
          <ErrorView
            error={state.error}
            chinese={chinese}
            onRetry={retryCallback}
          />
        </Rendered>
      ) : state.handle && root ? (
        <Rendered onRendered={onRendered}>
          <AgentGuiErrorBoundary
            key={attempt}
            chinese={chinese}
            onRetry={retryCallback}
          >
            <ProfilerBinding1
              state={state}
              client={client}
              initial={initial}
              root={root}
            />
          </AgentGuiErrorBoundary>
        </Rendered>
      ) : (
        <AgentGUIStartupShell loadingLabel="Loading Agent GUI" />
      )}
    </div>
  );
}

export default AgentGuiMount;

const ProfilerBinding1 = observer(function ProfilerBinding1({
  state,
  client,
  initial,
  root
}: ProfilerBinding1Props) {
  const profile = useRef<ReturnType<typeof installConversationUiProfile> | null>(null);
  useEffect(() => {
    if (!import.meta.env.DEV || !state.handle) return;
    const current = installConversationUiProfile(root, client, state.handle, initial);
    profile.current = current;
    return () => {
      current.dispose();
      if (profile.current === current) profile.current = null;
    };
  }, [root, client, state.handle, initial]);
  const onRenderProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Profiler>["onRender"]>
  >((_id, _phase, actualDuration) => {
    state.handle?.rendered(actualDuration);
    profile.current?.rendered(actualDuration);
  });
  return (
    <Profiler id="agent-gui" onRender={onRenderProp2}>
      <AgentGuiApplication
        client={client}
        initial={initial}
        handle={state.handle!}
        root={root}
      />
    </Profiler>
  );
});

type ProfilerBinding1Props = {
  state: { handle: Handle | null; error: Error | null };
  client: NonNullable<AgentGuiMountProps>["client"];
  initial: NonNullable<AgentGuiMountProps>["initial"];
  root: HTMLDivElement;
};
