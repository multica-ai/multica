# Dialog

Import from `@multica/ui/components/ui/dialog`.

## Usage

Use Dialog for a focused task that needs a modal surface. Use Popover for contextual options and AlertDialog for a consequential confirmation.

Compose `Dialog`, `DialogTrigger`, `DialogContent`, `DialogHeader`, `DialogTitle`, `DialogDescription`, `DialogFooter` and `DialogClose`. Dialog has no `variant` prop; form and long-content examples are compositions of the same component.

## Behavior

Provide a title and a concise description. Let the primitive manage focus trapping, Escape, outside dismissal and focus restoration. Use `DialogTrigger` so focus can return to the opener. Label form fields. Keep the footer reachable with long content and narrow viewports.

Only close a submitting dialog after the operation succeeds. Preserve its input and show an error on failure. The UI Lab save interaction is a local demonstration; it makes no API request.

## Correct

```tsx
<Dialog>
  <DialogTrigger render={<Button />}>Edit title</DialogTrigger>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>Edit title</DialogTitle>
      <DialogDescription>Update the task title.</DialogDescription>
    </DialogHeader>
    <label>Title<Input defaultValue="Review interface" /></label>
    <DialogFooter>
      <DialogClose render={<Button variant="outline" />}>Cancel</DialogClose>
      <Button onClick={saveThenClose}>Save</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
```

## Avoid

```tsx
<div role="dialog" className="fixed inset-0">
  <Input placeholder="Title" />
  <Button onClick={() => { save(); close(); }}>Save</Button>
</div>
```

A role alone does not supply an accessible name, focus management or dismissal behavior. Do not close before an asynchronous save succeeds.

## Motion

Popup and backdrop consume `--dialog-enter-duration`, `--dialog-exit-duration`, `--dialog-enter-easing` and `--dialog-exit-easing` from `packages/ui/styles/tokens.css`. The defaults preserve the existing 100ms/ease animation. The popup also fades and scales between 95% and 100%.

Changes affect shared Dialog instances on web and desktop, including compositions that use DialogContent, unless callers override the animation. They do not change Popover, Tooltip, AlertDialog or JavaScript motion constants.

Respect `prefers-reduced-motion`: popup and backdrop animations are disabled. UI Lab playback speed is preview-only and never exported. Open, close and replay the real component; also interrupt it with rapid open/close actions.

## Application

Compare both themes and keyboard behavior. Save a scheme for later or export CSS and merge into the matching `:root` block in `packages/ui/styles/tokens.css`. Exporting does not write to product code.
