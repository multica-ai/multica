import { appTasks } from '@ohos/hvigor-ohos-plugin';
import { createRNOHProjectPlugin } from '@rnoh/hvigor-plugin';

export default {
  system: appTasks,
  plugins: [createRNOHProjectPlugin({ nodeModulesPath: '../node_modules' })],
};
