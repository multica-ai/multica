import type { AuthDict } from "./types";

export function createEnDict(): AuthDict {
  return {
    loginPage: {
      title: "Sign in to Multica",
      description: "Enter your email to get a login code",
      emailLabel: "Email",
      emailPlaceholder: "you@example.com",
      continue: "Continue",
      sendingCode: "Sending code...",
      orDivider: "or",
      continueWithGoogle: "Continue with Google",
      emailRequired: "Email is required",
      sendCodeFailed: "Failed to send code. Make sure the server is running.",
    },
    codePage: {
      title: "Check your email",
      description: "We sent a verification code to",
      invalidCode: "Invalid or expired code",
      resendCode: "Resend code",
      resendIn: (seconds) => `Resend in ${seconds}s`,
      resendFailed: "Failed to resend code",
      back: "Back",
    },
    cliConfirm: {
      title: "Authorize CLI",
      descriptionPrefix: "Allow the CLI to access Multica as ",
      descriptionSuffix: "?",
      authorize: "Authorize",
      authorizing: "Authorizing...",
      useDifferentAccount: "Use a different account",
      authorizeFailed: "Failed to authorize CLI. Please log in again.",
    },
  };
}
