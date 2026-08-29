export type CallbackError =
  | {
      kind:
        | "missing_code"
        | "access_denied"
        | "login_failed"
        | "account_disabled"
        | "signup_prohibited"
        | "email_not_allowed"
        | "google_account_no_email"
        | "oauth_code_invalid";
    }
  | { kind: "raw"; text: string };

export function callbackErrorFrom(
  code: string | undefined,
  clientMessage: string | undefined,
): CallbackError {
  switch (code) {
    case "account_disabled":
      return { kind: "account_disabled" };
    case "signup_prohibited":
      return { kind: "signup_prohibited" };
    case "email_not_allowed":
      return { kind: "email_not_allowed" };
    case "google_account_no_email":
      return { kind: "google_account_no_email" };
    case "oauth_code_invalid":
      return { kind: "oauth_code_invalid" };
    default:
      break;
  }

  return clientMessage
    ? { kind: "raw", text: clientMessage }
    : { kind: "login_failed" };
}
