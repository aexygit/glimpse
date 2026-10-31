# Glimpse

Glimpse is a desktop app that inspects Aion 2 gear. It reads gear data from the game's network traffic on your machine and shows item stats, and it fetches item data from the NC official site JSON API (Taiwan catalog, in English) via github.com/nuriland/aion2-api. Everything else works offline.

## Requirements

- Windows. Capture uses pktmon (built in, no Npcap), so Glimpse runs as Administrator and asks for elevation at launch.
- Capture is passive and reads TCP traffic on every network adapter, VPN tunnels included, to find the game connection. Nothing is recorded to disk and only Aion 2 frames are decoded.

## Usage

1. Download `glimpse.exe` from the latest [release](https://github.com/aexygit/glimpse/releases/latest).
2. Accept the UAC prompt: pktmon capture needs Administrator.
3. Launch Aion 2. Gear and grades appear for characters who come into view around you; there is nothing to click in game.

Only fetching item data needs the network (it is cached on disk afterwards); capture works offline. Until an item is in the cache it shows as `Unknown item #ID`, and a character with no cached item is reported as not graded rather than given a grade the data cannot support. "Download all assets" in Settings fetches the whole catalog up front.

## Development

Prerequisites: Go, Node 24, libpcap headers on Linux (`libpcap-dev`), and the Wails v3 CLI (`go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0`). On Linux, `wails3 dev` also needs webkit2gtk.

```
wails3 dev      # run with hot reload
wails3 build    # production build into bin/
go test ./internal/...
```

## License

MIT, see [LICENSE](LICENSE).

## Credits

- [github.com/nuriland/a2kit](https://github.com/nuriland/a2kit) (MIT): packet capture and wire framing.
- [github.com/nuriland/aion2-api](https://github.com/nuriland/aion2-api) (MIT): item data from the NC official API.
