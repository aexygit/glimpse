# Glimpse

Glimpse is a desktop app that inspects Aion 2 gear. It reads gear data from the game's network traffic on your machine and shows item stats, and it can fetch reference data from questlog when online. Everything else works offline.

## Requirements

- Windows. Capture uses pktmon (built in, no Npcap), so Glimpse runs as Administrator and asks for elevation at launch.

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

TBD.
