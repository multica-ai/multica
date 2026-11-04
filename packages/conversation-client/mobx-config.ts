import { configure } from "mobx";

// Every change goes through an action, so a batch of events reaches views once.
configure({ enforceActions: "always" });
