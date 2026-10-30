# Glimpse: Design

Date: 2026-10-30
Status: Draft, pending review
License: MIT

## Goal

Open source desktop app for Aion 2. When the user inspects any character in game, the app captures the inspect data from network traffic, displays the character's gear, grades it, and lists what to upgrade first.

Windows first. Linux and macOS later, with no rewrite.

## Constraints

- Fast, low memory, stable. A bad packet must never crash the app.
- Everything works offline except fetching game data from external sources.
- Built to grow: future features (history, DPS meter, ...) plug in as new packages consuming the same decoded messages.
- Code, comments and identifiers in English. No em dashes anywhere. Comments only where the code is not obvious, and short.

## Non-goals (v1)

- Persistent history (session only, model ready for it later).
- Memory reading or any client modification.
- Exact upgrade cost paths (materials, kinah).
- Linux and macOS builds (kept compilable, not shipped).
- Frontend automated tests.

## Stack

- Go backend, Wails (latest stable) desktop shell using the system webview (WebView2 on Windows).
- Frontend: Svelte + TypeScript, built with Vite.
- Packet capture and framing: `github.com/nuriland/a2kit` (MIT). On Windows it uses pktmon, built into Windows: no Npcap, but the app runs as Administrator. On Linux it uses libpcap.
- Logging: `log/slog` to a rotating file in the user config folder.

## Architecture

```
glimpse/
  main.go              Wails entry point
  internal/
    capture/           thin wrapper over a2kit: live capture, status, replay of recordings
    protocol/          decodes game packets into typed messages (InspectResult)
    gear/              domain types: Character, Item, Slot, Stats (no I/O)
    gamedata/          item DB: local cache, fetch from NC official API (TW catalog), bulk asset download
    grading/           pure scoring: slot grades, overall score, upgrade advice
    app/               Wails bindings, in-memory session, UI events
  data/grading/        stat weights and BIS tables per class (JSON, embedded)
  frontend/            Svelte + TS
```

Dependencies flow one way: `app` -> `capture`, `protocol`, `gamedata`, `grading` -> `gear`. `gear` and `grading` do no I/O.

### Data flow

1. `capture` emits reassembled TCP payloads from the game connection.
2. `protocol` frames and decodes them. Only known opcodes are decoded, the rest is dropped. Inspect responses become `InspectResult` (character info, item IDs, enchant, rolled stats, sockets).
3. `app` enriches items through `gamedata` (name, icon, tier, base stats) and builds a `gear.Character`.
4. `grading` computes the report.
5. `app` stores character and report in the session and emits an `inspect` event.
6. The UI renders it.

### Capture

- Find the game process by executable name, list its TCP connections by PID (`GetExtendedTcpTable` on Windows), and set a BPF filter on those ports. No blind scanning.
- Poll for the process; reattach automatically if the game restarts or the connection changes.
- a2kit's wire decoder finds the game's connection among all traffic and handles reassembly, LZ4 and bundled frames. If capture fails for lack of rights, report a status the UI turns into "Run Glimpse as Administrator".
- Runs in its own goroutine under a `context`. A `recover` at the loop boundary logs and continues.

### Protocol

- Packet layout is confirmed against real captures early in implementation. a2kit proves the traffic is decodable; it has no inspect decoder yet, so Glimpse adds one over a2kit frames (candidate for an upstream contribution).
- Decoders are pure functions over byte slices, tested with recorded fixtures in `testdata/`.

## Domain model (`gear`)

```go
type Character struct {
    Name  string
    Class Class
    Level int
    Items map[Slot]Item
}

type Item struct {
    ID      int     // game item ID
    Name    string  // from gamedata
    Tier    int     // rarity rank, higher is better
    Enchant int
    Stats   Stats   // base + enchant + rolls, summed
    Sockets []Stats // manastones or equivalent
}

type Stats map[Stat]float64
```

`Class`, `Slot` and `Stat` are string backed. Which ones matter is decided by grading data, so a patch adding a stat is a data change only.

## Grading

Rules per class in `data/grading/<class>.json`:

```json
{
  "class": "assassin",
  "weights": { "attack": 1.0, "crit": 0.9, "accuracy": 0.6, "hp": 0.2 },
  "bis": { "weapon": { "itemId": 123, "tier": 5, "enchant": 15, "stats": { "attack": 1200, "crit": 80 } } },
  "thresholds": { "S": 0.95, "A": 0.85, "B": 0.70, "C": 0.50 }
}
```

- BIS entries carry their own tier and stats, so grading never needs `gamedata`.
- Weighted value of an item: `sum(stat * weight)` over its stats and sockets.
- Slot score: `weighted(item) / weighted(bis)`, capped at 1. Empty slot scores 0.
- Slot share: `weighted(bis of slot) / sum(weighted(bis of all slots))`.
- Overall score: `sum(slotScore * slotShare)`.
- Letter grade from `thresholds` (below the lowest threshold is `D`), for slots and overall.
- Upgrade advice: per slot `gain = (1 - slotScore) * slotShare`. Top 3 shown with a reason: missing item, enchant below BIS, lower tier, or weak rolls (same tier and enchant as BIS but lower weighted value).

Rules files are embedded in the binary. A file with the same name in the user config folder overrides the embedded one.

Initial weights are placeholders to be tuned by the maintainer and community.

## Game data (`gamedata`)

- Single `items.json` in `os.UserCacheDir()/glimpse`, loaded into a map at startup.
- Unknown item ID: fetched in background from the item source, cached, then an update event is emitted. Offline: shown as `Unknown item #ID` and still graded on its captured stats.
- Settings action "Download all assets": bulk fetch of all items and icons into the cache, with progress events. Icons are served to the webview from the cache.
- Requests are rate limited with an identifying User-Agent.
- Item source: NC official AION 2 site API, TW region catalog in English, through the MIT Go client `github.com/nuriland/aion2-api`. questlog.gg does not allow automated use. Global regions have no item catalog, so Global players rely on item IDs being shared with TW (verify with first captures). Datamining the client files stays the fallback.

## UI

- Main view: paper doll gear grid. Each slot shows icon, enchant and a grade badge. Hover shows item stats. Side panel shows overall grade, score and top 3 upgrades.
- Top bar: capture status (needs Administrator, waiting for game, capturing, error) and the list of characters inspected this session.
- Settings: asset download, cache folder path.

## Error handling

- Packet decode errors: logged at debug level, packet skipped.
- Network errors in `gamedata`: logged, item stays unknown, retried on next need.
- Corrupt cache file: logged, renamed to `.bad`, rebuilt.
- Invalid user grading override: logged, embedded rules used, warning shown in UI.

## Testing

- `protocol`: table driven tests on binary fixtures.
- `grading`: table driven tests on hand built characters.
- `gamedata`: tests against `httptest` servers, no real network.
- CI: GitHub Actions running `go test`, `golangci-lint`, and a Windows Wails build on each PR.
