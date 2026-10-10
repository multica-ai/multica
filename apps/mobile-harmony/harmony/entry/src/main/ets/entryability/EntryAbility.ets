/**
 * Copyright (c) 2025-2026 Huawei Technologies Co., Ltd.
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */

import {RNAbility} from '@rnoh/react-native-openharmony';

export default class EntryAbility extends RNAbility {
  override getPagePath() {
    return 'pages/Index';
  }

  // AnyThread turbo modules (AsyncStorage) resolve through the worker
  // provider; the MAIN-thread instance-ref hand-off drops them
  // ("Couldn't find Turbo Module on the ArkTs side" at launch).
  override getRNOHWorkerScriptUrl() {
    return "entry/ets/workers/RNOHWorker.ets"
  }
}
