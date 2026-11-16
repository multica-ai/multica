import type { ComponentProps } from "react";
import { KeyboardAvoidingView as NativeKeyboardAvoidingView, Platform } from "react-native";
import { KeyboardAvoidingView as ControlledKeyboardAvoidingView } from "react-native-keyboard-controller";
import { cssInterop } from "nativewind";

cssInterop(ControlledKeyboardAvoidingView, { className: "style" });

/** Android edge-to-edge windows need the controller's keyboard frame events. */
export function KeyboardAvoidingView(props: ComponentProps<typeof NativeKeyboardAvoidingView>) {
  return Platform.OS === "android" ? (
    <ControlledKeyboardAvoidingView {...props} behavior={props.behavior ?? "height"} />
  ) : (
    <NativeKeyboardAvoidingView {...props} />
  );
}
