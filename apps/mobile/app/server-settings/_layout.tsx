import { Stack } from "expo-router";

/**
 * 服务器设置分组。刻意不在 (app) / (auth) 之内 —— 未登录用户连自建后端
 * 是核心场景,登录前必须可达,因此这里没有任何 auth 守卫(RUYI-4)。
 *
 * 用原生 Stack header(而非根布局的 headerShown:false),这样返回按钮与
 * 侧滑返回都是系统行为,跟其他 push 屏一致。
 */
export default function ServerSettingsLayout() {
  return <Stack />;
}
