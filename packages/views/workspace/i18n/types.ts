export type WorkspaceDict = {
  createForm: {
    nameLabel: string;
    namePlaceholder: string;
    urlLabel: string;
    slugPlaceholder: string;
    submit: string;
    submitting: string;
    slugFormatError: string;
    slugConflictError: string;
    chooseDifferentSlug: string;
    createFailed: string;
  };
  noAccess: {
    title: string;
    description: string;
    goToWorkspaces: string;
    signInDifferent: string;
  };
  newWorkspace: {
    back: string;
    logOut: string;
    title: string;
    description: string;
    inviteHint: string;
  };
};
