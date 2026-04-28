export type InviteDict = {
  shell: {
    back: string;
    logOut: string;
  };
  notFound: {
    title: string;
    description: string;
    goToDashboard: string;
  };
  accepted: {
    titlePrefix: string;
    titleSuffix: string;
    redirecting: string;
  };
  declined: {
    title: string;
    description: string;
    goToDashboard: string;
  };
  invitation: {
    joinTitlePrefix: string;
    workspaceFallback: string;
    invitedAsAdminPrefix: string;
    invitedAsAdminSuffix: string;
    invitedAsMemberPrefix: string;
    invitedAsMemberSuffix: string;
    alreadyHandledAccepted: string;
    alreadyHandledDeclined: string;
    expired: string;
    accept: string;
    accepting: string;
    decline: string;
    declining: string;
    acceptFailed: string;
    declineFailed: string;
  };
};
