# Screenshots

Part of the [development guide](README.md).

`mise run screenshots` (`scripts/screenshots.sh`) regenerates the pictures in `docs/images/` that the README shows. It runs `git-tui -demo` in a real Ghostty window, so the avatar and PR pictures are drawn exactly as users see them, presses keys to reach each view, and captures the window. Run it whenever a change alters what the screen looks like, and commit `docs/images/`.

It cannot run in CI: it needs a Mac with a screen.

## One-off setup

1. **macOS and Ghostty.** Install [Ghostty](https://ghostty.org) in `/Applications`. The script opens its own Ghostty window with Ghostty's default settings, so your Ghostty configuration does not change the pictures. It also needs `cwebp` to save the pictures as WebP: `brew install webp`. Everything else it uses (`osascript`, `screencapture`, `sips`) comes with macOS.
2. **Screen Recording,** so the script can capture the window. Open System Settings › Privacy & Security › Screen Recording, and turn it on for the app you run the script from: your terminal (Ghostty, Terminal, iTerm2) or your editor if you use its terminal. Quit and reopen that app afterwards.
3. **Accessibility,** so the script can press keys in its window. In System Settings › Privacy & Security › Accessibility, turn it on for the same app.
4. **Automation,** on the first run. macOS asks whether your terminal may control System Events. Allow it. If you said no, turn it on in System Settings › Privacy & Security › Automation, under your terminal.

## Running it

```sh
mise run screenshots
```

A Ghostty window opens, steps through the views and closes, then a narrower one does the same, then a third: about 40 seconds in all. **Leave the keyboard and mouse alone while it runs.** Keystrokes go to whichever app is in front, so the script brings its window forward and checks it is in front before every key press; if anything else takes the front, it stops instead of typing into it. It prints each picture it writes:

```text
docs/images/overview.webp
docs/images/pull-requests.webp
docs/images/detail.webp
docs/images/comments.webp
docs/images/header-ascii.webp
docs/images/header-text.webp
docs/images/light.webp
docs/images/narrow.webp
docs/images/connections.webp
docs/images/diff.webp
docs/images/deploys.webp
docs/images/compare.webp
docs/images/add-repositories.webp
```

Look at the pictures before committing them: each should show a different screen.

## When it fails

| Message or symptom | Cause and fix |
|---|---|
| `needs macOS and /Applications/Ghostty.app` | Install Ghostty in `/Applications`. |
| `Ghostty window did not appear` | Ghostty did not open a window within 10 seconds. Run it again; if it keeps happening, open Ghostty once by hand to clear any first-launch prompt. |
| `The screenshot window is not in front, so no keys were sent` | Something else took the front: you clicked or typed, or a notification appeared. Run it again without touching anything. |
| `could not create image from window`, or pictures of only the desktop | Screen Recording is off for the app running the script. Turn it on (step 2) and reopen that app. |
| `osascript is not allowed to send keystrokes` | Accessibility is off for the app running the script (step 3). |
| `Not authorized to send Apple events to System Events` (macOS's spelling) | Automation was refused (step 4). |
| Every picture shows the first screen | The keys did not reach the window. Check Accessibility, then run it again hands off. |
