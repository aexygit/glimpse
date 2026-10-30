# Item data source

Decision: questlog.gg does not allow automated use (see questlog.md), so item data comes from NC's official AION 2 site API.

- Library: github.com/nuriland/aion2-api v0.5.0 (MIT), an official-API client with a built-in 5 req/s limiter, retry on 429/5xx, and an injectable *http.Client and User-Agent.
- Region/locale: `RegionTW`, `LocaleEN`. Global (NA/EU) has no item catalog route (`Item` is unsupported there, `ErrNoRoute` territory), so we assume Global item IDs equal TW IDs. Unverified; mismatches would show wrong names/stats.
- Routes used (TW origin https://tw.ncsoft.com):
  - `GET /aion2_tw/v2.0/dict/search/item?page&size&locale=en-US` : listing, ids only for our purposes. Capped at 10,000 rows by NC.
  - `GET /aion2/api/gameconst/item?lang=en&id=<id>&enchantLevel=0` : item definition: name, grade (Common, Rare, Legend, Unique, Epic), icon URL (assets.playnccdn.com), mainStats `[{id,name,value,minValue}]` with string values such as "627" or "5%".
- Mapping: Tier = grade id order 1..5; Stats = mainStats keyed by NC stat id (e.g. WeaponFixingDamage, PvEAmplifyDamage), "%" stripped, weapons use the max `value`. Rolled subStats are ignored (they are ranges; real rolls come from the inspect packets).
- Caveats: the English locale on the TW item route is assumed to be honoured (not verified live); the 10,000 cap may leave some items to be fetched lazily via `Item`.

Live check (2026-10-30, via the aion2 MCP, region tw, locale en, id 110120001): English name and stat labels returned, JSON shape identical to the library testdata used for fixtures.
