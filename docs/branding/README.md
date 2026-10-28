# AethCraft branding

The product uses the supplied AethCraft brand kit in `aethcraft/`. Its primary
color is `#214577`; the original light, dark, transparent, app icon, favicon,
social images and PDF guidelines are retained there.

Run `python scripts/sync-brand-assets.py docs/branding/aethcraft` with Pillow
installed to refresh derived assets. Wordmarks are cropped to their content,
given quarter-height clear space, and scaled proportionally. The browser icon
and desktop icon use the kit's icon artwork, not a miniature wordmark.

Product-facing titles, translated text, assistant prompts, small-program pages,
desktop dialogs, API descriptions and documentation use AethCraft. Historical
copyright notices remain in the license and third-party notices.

Compatibility identities remain stable: Go module/import paths, published
package and image names, CLI executable `weknora`, environment variable names,
database and volume names, existing local data directories, localStorage keys,
API headers, tool/agent IDs, and the `WeKnora` widget alias. Changing those would
require a separate data/integration migration. `AethCraft` is also available as
the widget global for new integrations.

WeKnora Cloud is an external service, so its actual provider name, logo and
service endpoint remain accurate. Upstream source/download/support links retain
their real URLs; an AethCraft public domain or release repository has not been
provided.

The local AethCraft product film is
`website-docs/homepage/public/docs/_home/brand/aethcraft-intro.mp4` (112 seconds,
1920×1080, English on-screen text, no narration). Its animation source is
`misc/intro-video/index.html`; `misc/intro-video/render.mjs` can render it with
Playwright and FFmpeg. README links and the homepage player use this local film.
