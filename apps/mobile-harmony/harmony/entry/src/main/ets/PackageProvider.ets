/**
 * Copyright (c) 2025-2026 Huawei Technologies Co., Ltd.
 *
 * This source code is licensed under the MIT license found in the
 * LICENSE file in the root directory of this source tree.
 */

import type { RNPackageContext, RNOHPackage } from '@rnoh/react-native-openharmony';
import { createRNOHPackages as createRNOHPackagesAutolinking } from './RNOHPackagesFactory'
import {
  ComponentBuilderContext,
  UITurboModule,
  UITurboModuleContext,
  AnyThreadTurboModule,
  AnyThreadTurboModuleContext,
} from '@rnoh/react-native-openharmony';
import { ReanimatedPackage } from '@react-native-oh-tpl/react-native-reanimated/ts'
import { MulticaPackage } from './multica/MulticaPackage'

type UITurboModuleByNameFactory = (ctx: UITurboModuleContext) => UITurboModule | null

// The oh-tpl packages for this matrix predate the RNOH 0.82 registration
// contract on both fronts:
//  1. Their packages extend an RNPackage base without the wrapped-builder
//     API, which fails ArkTS structural typing against RNOHPackage.
//  2. They register turbo modules by overriding createTurboModulesFactory /
//     createAnyThreadTurboModuleFactory, both deprecated no-ops on this
//     platform — the runtime builds factories from
//     getUITurboModuleFactoryByNameMap / getAnyThreadTurboModuleFactoryByNameMap,
//     so without the bridges below the app crashes at launch with
//     "Couldn't find Turbo Module on the ArkTs side" (RNCNetInfo was the
//     visible one; all four are bridged here). Drop a bridge once the
//     package ships against the current platform interface.
class ReanimatedPackageAdapter extends ReanimatedPackage {
  createWrappedCustomRNComponentBuilderByComponentNameMap(): Map<string, WrappedBuilder<[ComponentBuilderContext]>> {
    return new Map();
  }

  getUITurboModuleFactoryByNameMap(): Map<string, UITurboModuleByNameFactory> {
    const map = super.getUITurboModuleFactoryByNameMap()
    map.set('ReanimatedModule', (ctx: UITurboModuleContext) => {
      const legacyFactory = this.createTurboModulesFactory(ctx)
      return legacyFactory?.createTurboModule('ReanimatedModule') ?? null
    })
    return map
  }
}

// RNCNetInfo is registered in MulticaPackage (the netinfo tpl package's
// oh-package entry is broken, so its package class cannot be extended —
// the module class is imported by subpath there). The matching CPP proxy
// lives in cpp/PackageProvider.cpp.

export function getRNOHPackages(ctx: RNPackageContext): RNOHPackage[] {
  return [
    ...createRNOHPackagesAutolinking(ctx),
    new ReanimatedPackageAdapter(ctx),
    new MulticaPackage(ctx),
  ]
}
