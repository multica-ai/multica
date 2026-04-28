export type AuthDict = {
  loginPage: {
    title: string;
    description: string;
    emailLabel: string;
    emailPlaceholder: string;
    continue: string;
    sendingCode: string;
    orDivider: string;
    continueWithGoogle: string;
    emailRequired: string;
    sendCodeFailed: string;
  };
  codePage: {
    title: string;
    description: string;
    invalidCode: string;
    resendCode: string;
    resendIn: (seconds: number) => string;
    resendFailed: string;
    back: string;
  };
  cliConfirm: {
    title: string;
    descriptionPrefix: string;
    descriptionSuffix: string;
    authorize: string;
    authorizing: string;
    useDifferentAccount: string;
    authorizeFailed: string;
  };
};
