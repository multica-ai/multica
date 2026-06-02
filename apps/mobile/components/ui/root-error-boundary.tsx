import * as React from "react";
import { View } from "react-native";
import { SafeAreaView } from "react-native-safe-area-context";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { captureBoundaryError } from "@/lib/crash-reporting";

interface RootErrorBoundaryState {
  error: Error | null;
}

export class RootErrorBoundary extends React.Component<
  React.PropsWithChildren,
  RootErrorBoundaryState
> {
  state: RootErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): RootErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    captureBoundaryError(error, info.componentStack ?? undefined);
  }

  private retry = () => {
    this.setState({ error: null });
  };

  render() {
    if (!this.state.error) return this.props.children;

    return (
      <SafeAreaView className="flex-1 bg-background">
        <View className="flex-1 justify-center gap-5 px-6">
          <View className="gap-2">
            <Text variant="h3" className="text-left">
              Something went wrong
            </Text>
            <Text className="text-muted-foreground leading-6">
              Multica hit an unexpected app error. The session is still on this
              device, and you can retry the screen now.
            </Text>
          </View>
          <View className="gap-3">
            <Button onPress={this.retry}>
              <Text>Retry</Text>
            </Button>
          </View>
        </View>
      </SafeAreaView>
    );
  }
}
