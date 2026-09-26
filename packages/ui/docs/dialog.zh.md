# 弹窗

从 `@multica/ui/components/ui/dialog` 导入。

## 使用规则

Dialog 用于需要模态界面的独立操作。上下文选项使用 Popover；重要操作确认使用 AlertDialog。

组合使用 `Dialog`、`DialogTrigger`、`DialogContent`、`DialogHeader`、`DialogTitle`、`DialogDescription`、`DialogFooter` 和 `DialogClose`。Dialog 没有 `variant` 属性；表单和长内容只是同一组件的不同组合。

## 行为

提供标题和简短说明。由组件处理焦点限制、Escape、点击外部关闭和焦点恢复。使用 `DialogTrigger`，让焦点能返回打开按钮。表单字段需要标签。长内容和窄屏下，底部操作必须可访问。

提交成功后再关闭；失败时保留输入并显示错误。UI Lab 中的保存是本地演示，不发送 API 请求。

## 正确示例

```tsx
<Dialog>
  <DialogTrigger render={<Button />}>编辑标题</DialogTrigger>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>编辑标题</DialogTitle>
      <DialogDescription>更新任务标题。</DialogDescription>
    </DialogHeader>
    <label>标题<Input defaultValue="检查界面" /></label>
    <DialogFooter>
      <DialogClose render={<Button variant="outline" />}>取消</DialogClose>
      <Button onClick={saveThenClose}>保存</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
```

## 避免

```tsx
<div role="dialog" className="fixed inset-0">
  <Input placeholder="标题" />
  <Button onClick={() => { save(); close(); }}>保存</Button>
</div>
```

仅设置 role 不会提供无障碍名称、焦点管理和关闭行为。异步保存成功前不要关闭弹窗。

## 动效

弹窗和遮罩读取 `packages/ui/styles/tokens.css` 中的 `--dialog-enter-duration`、`--dialog-exit-duration`、`--dialog-enter-easing` 和 `--dialog-exit-easing`。默认保持现有的 100ms/ease 动画，弹窗同时淡入淡出并在 95% 和 100% 之间缩放。

参数影响网页端和桌面端的共享 Dialog，以及使用 DialogContent 的组合；调用方自行覆盖动画时除外。不影响 Popover、Tooltip、AlertDialog 和 JavaScript 动效常量。

遵循系统减少动态效果设置，禁用弹窗和遮罩动画。UI Lab 的播放速度仅用于预览，不参与导出。打开、关闭和重播真实组件，也要快速交替打开与关闭，检查中断效果。

## 应用修改

检查两种主题和键盘操作。保存方案，或导出 CSS 并合并到 `packages/ui/styles/tokens.css` 的 `:root` 区块。导出不会直接修改产品代码。
