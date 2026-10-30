# 按钮

从 `@multica/ui/components/ui/button` 导入。

## 使用规则

Button 用于执行操作，页面跳转使用链接。每个弹窗或表单保留一个突出显示的主要操作。

| Variant | 用途 |
| --- | --- |
| `default` | 保存、新建等主要操作 |
| `outline` | 取消等次要操作 |
| `secondary` | 柔和底色的普通操作 |
| `ghost` | 工具栏中弱化显示的操作 |
| `brand` | 已启用的筛选器等选中状态 |
| `brandSubtle` | 有活动但不需要突出选中的状态 |
| `destructive` | 删除等破坏性操作 |
| `link` | 链接样式的操作；页面跳转仍使用真实链接 |

## 尺寸与状态

文字尺寸：`xs`、`sm`、`default`、`lg`。图标尺寸：`icon-xs`、`icon-sm`、`icon`、`icon-lg`。

操作不可用时使用 `disabled`。提交中组合加载图标、`disabled` 和 `aria-busy`；Button 没有 `loading` 属性。纯图标按钮必须有无障碍名称。保留键盘焦点样式。

## 正确示例

```tsx
<Button type="submit" variant="default">保存</Button>
<Button type="button" variant="outline" onClick={onCancel}>取消</Button>
<Button variant="ghost" size="icon-xs" aria-label="添加条目"><Plus /></Button>
```

## 避免

```tsx
<Button variant="primary" loading>保存</Button>
<Button className="h-6 bg-blue-500 px-2">保存</Button>
```

不支持 `primary` 和 `loading`。优先使用现有尺寸和语义样式，再考虑覆盖大小或颜色。

## 参数与应用

`--button-height-*`、`--button-padding-*`、`--button-gap-*` 分别控制各尺寸。颜色使用语义 token。尺寸修改影响所有使用该尺寸的按钮；局部样式覆盖可能优先。

UI Lab 导出 `packages/ui/styles/tokens.css` 的 CSS 补丁。对比浅色、深色下的原始与修改效果，合并到对应区块，再检查代码差异。在浏览器中保存方案不会修改产品代码。
