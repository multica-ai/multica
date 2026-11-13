// These two must stay the first imports; see install-crypto.ts and install-storage.ts.
import "./platform/install-crypto";
import "./platform/install-storage";
import { registerRootComponent } from "expo";
import { installQueryLifecycle } from "./platform/app-lifecycle";
import { App } from "./ui/App";

installQueryLifecycle();
registerRootComponent(App);
