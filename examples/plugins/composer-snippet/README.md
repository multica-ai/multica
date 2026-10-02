# Markdown Snippet

This installable example adds `/snippet` to supported composers. It opens a
small modal with one static Markdown notes template. Choosing it inserts the
template into the current draft through `composer.insert`; the user reviews
and submits the draft through the normal composer.
The modal stays open to confirm insertion; close it to return to the draft.

The example requests no scopes, makes no API calls, and does not read draft
text. It demonstrates a composer command and the host's one-shot Markdown
insertion API without relying on workspace data or server-side Skill behavior.

## Install

Zip this folder, including `multica.plugin.json` and `ui/main.js`, then upload
it in **Settings → Plugins**. Type `/snippet` in a supported composer and
choose **Notes template** to insert it.
