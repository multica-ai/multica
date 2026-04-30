// Public surface of the Channels view package. Both web and desktop apps
// import from here and never reach into ./components/* directly, so this
// file is the contract.
export { ChannelsPage } from "./components/channels-page";
export { ChannelCreateDialog } from "./components/channel-create-dialog";
export { NewDMDialog } from "./components/new-dm-dialog";
export { MembersPanel } from "./components/members-panel";
export { ChannelSettingsDialog } from "./components/channel-settings-dialog";
export { ChannelList } from "./components/channel-list";
export { ChannelHeader } from "./components/channel-header";
export { ChannelMessageList } from "./components/channel-message-list";
export { ChannelComposer } from "./components/channel-composer";
export { MessageRow } from "./components/message-row";
