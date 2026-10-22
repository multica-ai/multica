# Button

Import from `@multica/ui/components/ui/button`.

## Usage

Use Button for actions. Use a navigation link for destinations. Keep one dominant action per dialog or form.

| Variant | Use |
| --- | --- |
| `default` | Main action, such as Save or Create |
| `outline` | Secondary action, such as Cancel |
| `secondary` | Neutral action with a soft fill |
| `ghost` | Low emphasis toolbar action |
| `brand` | Active control, such as an enabled filter |
| `brandSubtle` | Activity without a strongly selected state |
| `destructive` | Delete or another destructive action |
| `link` | An action styled like a link; use an actual link for navigation |

## Sizes and states

Text sizes: `xs`, `sm`, `default`, `lg`. Icon sizes: `icon-xs`, `icon-sm`, `icon`, `icon-lg`.

Use `disabled` when an action is unavailable. For pending actions, compose a spinner with `disabled` and `aria-busy`; Button has no `loading` prop. Icon-only buttons need an accessible name. Keep visible keyboard focus.

## Correct

```tsx
<Button type="submit" variant="default">Save</Button>
<Button type="button" variant="outline" onClick={onCancel}>Cancel</Button>
<Button variant="ghost" size="icon-xs" aria-label="Add item"><Plus /></Button>
```

## Avoid

```tsx
<Button variant="primary" loading>Save</Button>
<Button className="h-6 bg-blue-500 px-2">Save</Button>
```

`primary` and `loading` are not supported props. Choose a supported size and semantic variant before overriding geometry or colors.

## Tokens and application

`--button-height-*`, `--button-padding-*` and `--button-gap-*` control each size. Theme colors come from semantic tokens. Geometry changes affect every Button using that size; local class overrides can take precedence.

UI Lab exports a CSS patch for `packages/ui/styles/tokens.css`. Review the original and edited previews in both themes, merge declarations into the matching blocks, then review the source diff. Saving a browser design does not apply it to product code.
