# podcast-tui

**A fully-featured podcast player for your terminal.**

[![Go Version](https://img.shields.io/badge/go-1.21%2B-00ADD8?style=flat-square&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/license-MIT-brightgreen?style=flat-square)](LICENSE)
[![Platform](https://img.shields.io/badge/platform-macOS-lightgray?style=flat-square)]()

<!-- SCREENSHOT: Full-screen player view with album artwork and progress bar -->
> _Screenshot coming soon_

---

`podcast-tui` brings a complete podcast listening experience to the terminal — search the iTunes directory, save your favorites, stream episodes with pitch-preserving variable speed, and track your listening history, all without leaving the command line.

## Highlights

- **Search & discover** — query the iTunes podcast directory instantly
- **Save your favorites** — a personal library with fuzzy search across titles and categories
- **Smart home screen** — opens to your saved podcasts, sorted alphabetically, with new episode counts since your last session
- **Full audio control** — stream via ffmpeg with play/pause, 10s/30s seek, variable speed (0.5x–3.0x), and jump-to-timestamp
- **Album artwork in the terminal** — rendered inline using Unicode block characters
- **Automatic progress tracking** — resumes where you left off; episodes auto-complete at 95%
- **Listening stats** — total time listened and most-listened podcast, shown on every home screen
- **Episode descriptions** — HTML-stripped, scrollable inline, without leaving the app
- **Data export** — one command exports your history and saved podcasts as CSV files

---

## Screenshots

### Saved Podcasts

<!-- SCREENSHOT: Saved podcasts screen showing podcast list, new episode counts, and listening stats table -->
> _Screenshot coming soon_

### Episode List

<!-- SCREENSHOT: Episode list with date, title, and progress columns — some episodes faded gray (completed), one showing a percentage -->
> _Screenshot coming soon_

### Fuzzy Episode Search

<!-- GIF: Typing in the episode search dialog with results filtering in real time -->
> _GIF coming soon_

### Player View

<!-- SCREENSHOT: Full-screen player with album art, progress slider, and speed indicator -->
> _Screenshot coming soon_

### Episode Description

<!-- SCREENSHOT: Episode description modal overlay with scrollable HTML-stripped text -->
> _Screenshot coming soon_

---

## Installation

### Prerequisites

- **Go 1.21+** — [install Go](https://go.dev/dl/)
- **ffmpeg** — required for audio streaming

```sh
brew install ffmpeg
```

### Install

```sh
go install github.com/naveenvenkatesan/podcast-tui@latest
```

Or build from source:

```sh
git clone https://github.com/naveenvenkatesan/podcast-tui.git
cd podcast-tui
go build -o podcast-tui .
```

### Run

```sh
podcast-tui
```

If you built from source and haven't added the binary to your `PATH`:

```sh
./podcast-tui
```

---

## Features

### Podcast Discovery

Search the iTunes podcast directory by name. Results are returned instantly and navigable with keyboard or vim-style keys. Press `Enter` to open a podcast and browse its episodes.

### Saved Podcasts

Save any podcast with `Ctrl+S` for one-key access from your home screen. Saved podcasts are:

- Stored persistently in `~/.config/podcast-tui/saved.json`
- Sorted alphabetically by title
- Enriched with artwork and category metadata from each podcast's RSS feed
- Annotated with **new episode counts** — `podcast-tui` tracks when you last opened the app and highlights how many episodes have been published since

Press `Ctrl+U` to unsave. Use `s` to fuzzy-search your library by title or category in real time.

### Episode Browsing

Episodes are displayed in a paginated three-column list: **date**, **title**, and **progress**. Completed episodes appear in faded gray. In-progress episodes show a playback percentage.

Press `s` to open a fuzzy search dialog and filter the episode list by title. Results are ranked by match quality and update as you type. Press `Esc` to clear the filter.

### Playback

`podcast-tui` streams audio through ffmpeg and supports:

| Feature | Detail |
|---|---|
| Play / Pause | `Space` |
| Seek back | `←` — 10 seconds |
| Seek forward | `→` — 30 seconds |
| Variable speed | `+` / `-` — 0.5x to 3.0x, pitch-preserving |
| Jump to timestamp | `g` — enter `MM:SS` or `HH:MM:SS` |
| Auto-resume | Picks up from your last position |
| Auto-complete | Episodes marked done at 95% |

Seeking is seamless — `podcast-tui` pre-buffers the next read so there are no audio gaps during position changes.

### Player View

Press `p` from any screen to open the full player. It displays:

- **Album artwork** rendered in color using Unicode block characters
- Episode title and podcast name
- A wide progress slider showing current position and total duration
- Playback speed when playing at a non-1x rate

### Now Playing Bar

A persistent footer appears on every screen except the player, showing the current episode title, a visual progress slider, and playback time. Press `Space` to play/pause without leaving what you're doing, or `p` to jump to the full player view.

### Progress Tracking

Progress is saved automatically every ~500ms to `~/.config/podcast-tui/history.json`. Each entry records the episode title, podcast title, position in seconds, completion state, and a last-listened timestamp.

- Mark an episode played manually with `m`; unmark with `u`
- Clear all history with `Ctrl+D` (prompts for confirmation)

### Listening Stats

Your listening history is automatically summarized and displayed on the search and saved podcasts screens:

- **Total listening time** — formatted as years, days, hours, and minutes
- **Most-listened podcast** — the one you've spent the most time with

### Episode Descriptions

Press `d` on any episode to open its description in a scrollable modal. HTML is stripped and text is wrapped to a readable width. Scroll with `↑`/`↓` or `k`/`j`; close with `Esc`.

### Data Export

Press `Ctrl+E` from the search or saved podcasts screen to export your data as two CSV files to `~/Downloads`:

| File | Contents |
|---|---|
| `podcast-tui-history-YYYY-MM-DD.csv` | Episode title, podcast title, status, progress (seconds), last listened, feed URL |
| `podcast-tui-saved-YYYY-MM-DD.csv` | Podcast title, feed URL, artwork URL, categories (pipe-delimited) |

---

## Keyboard Reference

### Global

| Key | Action |
|---|---|
| `q` / `Ctrl+C` | Quit |
| `Esc` | Back / cancel |
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `p` | Jump to player (when audio is loaded) |
| `Space` | Play / pause (when audio is loaded) |

### Search Screen

| Key | Action |
|---|---|
| `Enter` | Search iTunes |
| `Ctrl+K` | Play / pause current episode |
| `Ctrl+E` | Export data to CSV |
| `Ctrl+D` | Clear listening history |
| `Esc` | Return to saved podcasts |

### Saved Podcasts Screen

| Key | Action |
|---|---|
| `Enter` | Open podcast episodes |
| `s` | Fuzzy search saved podcasts |
| `/` | Go to search screen |
| `Ctrl+E` | Export data to CSV |
| `Ctrl+D` | Clear listening history |

### Episode List

| Key | Action |
|---|---|
| `Enter` | Play selected episode |
| `s` | Fuzzy search / filter episodes |
| `d` | View episode description |
| `m` | Mark episode as played |
| `u` | Unmark episode |
| `Ctrl+S` | Save podcast |
| `Ctrl+U` | Unsave podcast |
| `Esc` | Go back |

### Player

| Key | Action |
|---|---|
| `Space` | Play / pause |
| `←` | Seek back 10 seconds |
| `→` | Seek forward 30 seconds |
| `+` / `=` | Increase playback speed |
| `-` | Decrease playback speed |
| `g` | Jump to timestamp (`MM:SS` or `HH:MM:SS`) |
| `Esc` | Return to episode list |

---

## Data & Configuration

All data is stored in `~/.config/podcast-tui/`:

| File | Purpose |
|---|---|
| `saved.json` | Saved podcasts with metadata |
| `history.json` | Listening progress and completion state |
| `meta.json` | Last-opened timestamp (used for new episode detection) |

Exports go to `~/Downloads/` (falls back to `~/` if Downloads doesn't exist).

---

## Contributing

Issues and pull requests are welcome. Please open an issue first for substantial changes so we can align on approach.

## License

[MIT](LICENSE) — © 2026 Naveen Venkatesan
