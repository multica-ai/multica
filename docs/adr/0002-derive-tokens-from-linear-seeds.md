# Derive web/desktop tokens from Linear seeds

Web and desktop keep Multica CSS names and Base UI components. Colour values come from a single Linear-like skin (accent, foundation, ink) run through our OKLCH contrast solver in `packages/ui/appearance`. Native mobile and a contrast slider are out of scope.

**Why not** hand-edit `tokens.css`: hover/active and the surface ladder drift independently. **Why not** import the other design-system package: it is AppKit/RN chrome, not this stack.
