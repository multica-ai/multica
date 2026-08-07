// Re-export — hook lives in @multica/cerebro-channels so the Chat page can
// mark a thread read without depending on the dynamic inbox package (FIR-4649).
export {
  useMarkThreadRead,
  markThreadReadInInbox,
  type MarkThreadReadVars,
} from "@multica/cerebro-channels";
