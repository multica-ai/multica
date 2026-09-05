# Competitor Inventory — cloudflare-site

## Reference sources

| Role | URL | Notes |
|------|-----|-------|
| Primary | https://developers.cloudflare.com/pages/ | Docs / hello patterns |
| Fallback | (intake-specific competitor) | Research phase fills this |

## Pages / routes

| Route | Purpose | In MVP? |
|-------|---------|---------|
| `/` | Core tool or landing | yes |
| `/privacy` | Privacy | yes |
| `/terms` | Terms | yes |

## Components (home minimum)

| ID | Component | Visible content / behavior | Breakpoints |
|----|-----------|----------------------------|-------------|
| C-nav | Brand / title area | Site name visible | desktop, 375 |
| C-hero | Hero / H1 | Primary headline | desktop, 375 |
| C-main | Core UI or CTA | Interactive or clear CTA | desktop, 375 |

## Interaction matrix

| ID | Action | Expected |
|----|--------|----------|
| I-load | Open `/` | Hero + main render, no blank screen |
| I-cta | Use core control or CTA | Observable feedback |
| I-mobile | Viewport 375 | Usable, no horizontal overflow |
