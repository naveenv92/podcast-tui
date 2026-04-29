# podcast-tui

A terminal-based podcast player written in Go.

## Features

### Saved Podcasts
- App opens to a saved podcasts page if any podcasts have been saved; otherwise opens to search
- Navigate saved podcasts with `↑/↓` or `k/j`
- Press `Enter` to open a saved podcast's episode list
- Press `/` to go to the search page

### Search
- Search the iTunes podcast directory by name
- Navigate results with `↑/↓` or `k/j` and press `Enter` to open
- Press `Esc` to return to saved podcasts (if any are saved)

### Episode List
- Paginated episode list with date, title, and playback progress columns
- Played episodes are shown in gray; in-progress episodes show a percentage
- Filter episodes by title with `s`
- Mark an episode as played with `m`; unmark with `u`
- Save the current podcast with `ctrl+s`; unsave with `ctrl+u`
- Press `Enter` to play the selected episode
- Press `Esc` to go back

### Player
- Streams audio via ffmpeg
- Play/pause with `Space`
- Seek back 10 seconds with `←`; seek forward 30 seconds with `→`
- Adjust playback speed with `+`/`-` (0.5x – 3.0x, pitch-preserving)
- Jump to a specific timestamp with `g` (accepts `MM:SS` or `HH:MM:SS`)
- Resume from last position for in-progress episodes
- Episodes auto-marked as completed at 95% playback
- Press `Esc` to return to the episode list

### Now Playing Bar
- Persistent footer on all screens except the player showing the current episode title, progress slider, and playback time
- Press `p` to jump to the player view
- Press `Space` (or `ctrl+k` on the search page) to play/pause without leaving the current screen

### Listening History
- Progress and completion state are saved automatically to `~/.config/podcast-tui/history.json`
- Listening stats (total time, most-listened podcast) shown on the search page
- Clear history with `ctrl+d` on the search page

### Saved Podcasts Persistence
- Saved podcasts are stored in `~/.config/podcast-tui/saved.json`

## Requirements

- Go 1.21+
- [ffmpeg](https://ffmpeg.org/) (`brew install ffmpeg` on macOS)

## Usage

```
go run .
```
