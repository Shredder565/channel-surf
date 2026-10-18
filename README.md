# Channel Surfer

An old-school cable-guide app for streaming TV. Browse channels on a classic blue-and-gold program grid, then jump straight into the streamer to watch.

## Screens

| Tab | What it shows | Data source |
|---|---|---|
| **Pluto TV** | Every Pluto TV channel with the next 6 hours of listings, channel logos and categories. Type a channel number (e.g. `1-2-0`) to jump to it. | Pluto TV's public guide service, fetched live at runtime |
| **YouTube TV** | ~75 networks from the YouTube TV base lineup with series / prime-time listings | [TVmaze](https://www.tvmaze.com) public schedule API (CC BY-SA 4.0), refreshed every 10 minutes |
| **Sports** | Where today's games are streaming, with a live tracker and auto-surf preview | Snapshot data (see *Limitations*) |
| **Accounts** | Sign in to each streamer once so Watch links open already signed in | Nothing stored by the app — see *Accounts & privacy* |

**Watch** opens the channel or game in the streamer's own player, in its own window.

## Controls

| Input | Action |
|---|---|
| Mouse wheel | Scroll channels row by row |
| Shift + wheel / sideways swipe | Move through time |
| ↑ ↓ / Page Up, Page Down | Scroll one row / one screen |
| ← → | Earlier / later |
| 0–9 | Go to channel (Pluto TV) |
| Enter | Watch the selected channel |
| F | Favorite the selected channel |
| Tab | Next screen |

## Build

Requires Go 1.24+. No other dependencies.

```bash
# Windows .exe (cross-compiles from any OS)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H windowsgui" -o ChannelSurfer.exe .

# Run locally on macOS / Linux
go run .

# Tests
go test ./...
```

The binary embeds everything in `web/`, serves it on `127.0.0.1:47815`, and opens it in an app-style Edge or Chrome window (falling back to the default browser). It exits a few seconds after the window is closed.

## How it works

```
main.go     launcher: local web server, app window, /api/guide + /api/yttv endpoints, log file
pluto.go    Pluto TV lineup + schedule (current guide service, with fallback to the legacy endpoint)
yttv.go     YouTube TV lineup + TVmaze schedule matching
web/        the guide UI (single HTML file, no build step) and bundled fonts
```

Errors are written to `ChannelSurfer.log` in the system temp folder.

## Accounts & privacy

The app runs its streamers inside a dedicated browser profile at `%LOCALAPPDATA%\ChannelSurfer\Profile`. Sign-ins and any passwords you choose to save live there, handled by the browser's own password manager. Channel Surfer never sees, stores or transmits credentials. Delete that folder to sign out of everything.

## Limitations

- **YouTube TV** has no public guide API. TVmaze covers series well but misses much live sports, news and movie programming — those slots show as "not listed". Watch opens YouTube TV's live guide rather than a specific channel.
- **Pluto TV** endpoints are undocumented and may change; the app tries two and logs failures.
- **Sports** listings are a hard-coded snapshot from Oct 18, 2026. A live version needs a scores/rights data feed.
- Windows SmartScreen will warn on first launch because the binary is unsigned.

## Credits

- Listings: [TVmaze](https://www.tvmaze.com) (CC BY-SA 4.0) and Pluto TV.
- Fonts: [VT323](https://fonts.google.com/specimen/VT323) and [Barlow Condensed](https://fonts.google.com/specimen/Barlow+Condensed), SIL Open Font License (see `web/fonts/`).

Channel Surfer is an independent project and is not affiliated with Pluto TV, YouTube TV, or any streaming service.
