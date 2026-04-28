export type ModalsDict = {
  common: {
    expand: string;
    collapse: string;
    close: string;
    cancel: string;
    creating: string;
  };
  createIssue: {
    srTitle: string;
    breadcrumb: string;
    titlePlaceholder: string;
    descriptionPlaceholder: string;
    moreOptions: string;
    parentChip: (identifier: string) => string;
    parentMenuItem: (identifier: string) => string;
    setParent: string;
    addSubIssue: string;
    removeParent: string;
    removeParentAria: string;
    childChip: (identifier: string) => string;
    removeChildAria: (identifier: string) => string;
    parentPickerTitle: string;
    parentPickerDescription: string;
    childPickerTitle: string;
    childPickerDescription: string;
    submit: string;
    submitting: string;
    successTitle: string;
    viewIssue: string;
    failed: string;
    failedSubIssuesAll: string;
    failedSubIssuesPartial: (failed: number, total: number) => string;
    failedUpdateStatus: string;
  };
  createProject: {
    srTitle: string;
    breadcrumb: string;
    titlePlaceholder: string;
    descriptionPlaceholder: string;
    chooseIcon: string;
    leadFallback: string;
    leadPlaceholder: string;
    noLead: string;
    membersHeading: string;
    agentsHeading: string;
    noResults: string;
    submit: string;
    submitting: string;
    success: string;
    failed: string;
  };
  createWorkspace: {
    back: string;
    title: string;
    description: string;
  };
  feedback: {
    title: string;
    description: string;
    placeholder: string;
    waitForUploads: string;
    tooLong: string;
    success: string;
    failedFallback: string;
    sending: string;
    submit: string;
  };
  setParentIssue: {
    title: string;
    description: string;
    failed: string;
    success: (identifier: string) => string;
  };
  addChildIssue: {
    title: string;
    description: string;
    failed: string;
    success: (identifier: string) => string;
  };
  deleteIssueConfirm: {
    title: string;
    description: string;
    cancel: string;
    confirm: string;
    deleting: string;
    success: string;
    failed: string;
  };
  issuePicker: {
    placeholder: string;
    searching: string;
    noResults: string;
    typeToSearch: string;
  };
};
