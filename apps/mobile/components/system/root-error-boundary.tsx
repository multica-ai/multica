import {
  Component,
  useEffect,
  useState,
  type ErrorInfo,
  type ReactNode,
} from "react";
import { ScrollView, View } from "react-native";
import * as Clipboard from "expo-clipboard";
import { Ionicons } from "@expo/vector-icons";
import { router, type ErrorBoundaryProps } from "expo-router";

import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { i18n } from "@/lib/i18n";

function createErrorId(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

function errorDetails(error: Error, errorId: string): string {
  return [
    `Error ID: ${errorId}`,
    `Message: ${error.message || "(no message)"}`,
    error.stack ? `Stack:\n${error.stack}` : null,
  ]
    .filter(Boolean)
    .join("\n");
}

interface ErrorScreenProps {
  error: Error;
  errorId: string;
  onRetry: () => void;
  onGoHome: () => void;
}

function ErrorScreen({
  error,
  errorId,
  onRetry,
  onGoHome,
}: ErrorScreenProps) {
  // The route-level boundary can render before MobileI18nProvider mounts, so
  // read the singleton rather than a React context.
  const t = i18n.getFixedT(null, "common");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!copied) return;
    const timer = setTimeout(() => setCopied(false), 2000);
    return () => clearTimeout(timer);
  }, [copied]);

  const copyDetails = () => {
    void Clipboard.setStringAsync(errorDetails(error, errorId))
      .then(() => setCopied(true))
      .catch(() => {
        // Clipboard access can fail on restricted devices; the details below
        // are still selectable.
      });
  };

  return (
    <View className="flex-1 bg-background px-6">
      <ScrollView
        contentContainerStyle={{ flexGrow: 1, justifyContent: "center" }}
        showsVerticalScrollIndicator={false}
      >
        <View className="items-center gap-4">
          <View className="size-12 items-center justify-center rounded-full bg-destructive/10">
            <Ionicons
              name="alert-circle-outline"
              size={26}
              color="#dc2626"
            />
          </View>
          <Text className="text-center text-lg font-semibold text-foreground">
            {t("errors.unexpected_title")}
          </Text>
          <Text className="text-center text-sm text-muted-foreground">
            {t("errors.unexpected_body")}
          </Text>
        </View>

        <View className="mt-6 gap-1.5 rounded-lg border border-border bg-secondary px-3 py-2.5">
          <Text className="text-xs font-medium text-foreground">
            {t("errors.error_id", { id: errorId })}
          </Text>
          <Text
            selectable
            className="text-xs text-muted-foreground"
            numberOfLines={4}
          >
            {error.message || t("errors.unknown_message")}
          </Text>
        </View>

        <View className="mt-6 gap-2">
          <Button onPress={onRetry}>
            <Text>{t("actions.retry")}</Text>
          </Button>
          <Button variant="outline" onPress={onGoHome}>
            <Text>{t("actions.go_home")}</Text>
          </Button>
          <Button variant="ghost" onPress={copyDetails}>
            <Text>
              {copied ? t("errors.copied") : t("errors.copy_details")}
            </Text>
          </Button>
        </View>
      </ScrollView>
    </View>
  );
}

interface State {
  error: Error | null;
  errorId: string | null;
}

/**
 * Catches render/lifecycle errors from the navigation tree and replaces the
 * blank screen with a recoverable one. `app/_layout.tsx` also exports an
 * expo-router `ErrorBoundary` backed by the same screen, which covers errors
 * thrown inside the root layout's own subtree.
 */
export class RootErrorBoundary extends Component<
  { children: ReactNode },
  State
> {
  state: State = { error: null, errorId: null };

  static getDerivedStateFromError(error: Error): State {
    return { error, errorId: createErrorId() };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error(
      "[root-error-boundary]",
      this.state.errorId,
      error,
      info.componentStack,
    );
  }

  private retry = () => {
    this.setState({ error: null, errorId: null });
  };

  private goHome = () => {
    this.retry();
    navigateHome();
  };

  render() {
    if (this.state.error) {
      return (
        <ErrorScreen
          error={this.state.error}
          errorId={this.state.errorId ?? "unknown"}
          onRetry={this.retry}
          onGoHome={this.goHome}
        />
      );
    }
    return this.props.children;
  }
}

/**
 * Expo Router route boundary. The router passes the thrown error and its own
 * retry callback; the id stays stable for the lifetime of this boundary so
 * the user can quote it when reporting.
 */
export function RouteErrorBoundary({ error, retry }: ErrorBoundaryProps) {
  const [errorId] = useState(createErrorId);

  useEffect(() => {
    console.error("[root-error-boundary:route]", errorId, error);
  }, [error, errorId]);

  return (
    <ErrorScreen
      error={error}
      errorId={errorId}
      onRetry={() => {
        void retry();
      }}
      onGoHome={() => {
        void retry();
        navigateHome();
      }}
    />
  );
}

function navigateHome() {
  try {
    router.replace("/");
  } catch {
    // The router may not be mounted if the root layout itself failed.
  }
}
