# questlog.gg research (Task 4 gate) - 2026-10-30

Status: BLOCKED. No documented public API found, and the terms could not be read.

## Evidence
- questlog.gg is a client-rendered Nuxt SPA (`data-ssr="false"`, `__NUXT_DATA__` is `[{"serverRendered":1},false]`). Every path returns the same HTML shell with HTTP 200, including /robots.txt, /sitemap.xml, /terms-of-service, /en/terms, /api/aion-2/items and /aion-2/en/database/items. So there is no real robots.txt (SPA fallback) and no terms text without running the JS app.
- The entry bundle (cdn.questlog.gg/_static/index/_nuxt/DRXm0e1i.js) names no API host or route. api.questlog.gg does not resolve.
- Web search finds no questlog.gg public API docs. Other Aion 2 DBs (metabot.gg, aion2hub.com) state they read game files directly.
- Site config lists contact@questlog.gg and a Discord, the only channels to ask permission.

## Not established
- Terms of service wording on automated access / scraping.
- Any JSON endpoint, item JSON shape, or rate limits.

## Next
Ask the user: (a) contact questlog.gg for permission and endpoint info, (b) inspect the SPA's network calls in a browser and read the ToS there, or (c) use the spec fallback (datamining).
