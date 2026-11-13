// Must stay the first import; see install-storage.ts.
import "./src/platform/install-storage";
import { registerRootComponent } from "expo";
import { App } from "./src/ui/App";
import { installQueryLifecycle } from "./src/platform/app-lifecycle";

installQueryLifecycle();
registerRootComponent(App);
