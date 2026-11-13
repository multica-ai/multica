// Must stay the first import; see install-storage.ts.
import "./platform/install-storage";
import { registerRootComponent } from "expo";
import { installQueryLifecycle } from "./platform/app-lifecycle";
import { App } from "./ui/App";

installQueryLifecycle();
registerRootComponent(App);
