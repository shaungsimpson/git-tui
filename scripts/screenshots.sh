#!/usr/bin/env bash
# Captures README screenshots of `git-tui -demo` in a real Ghostty window, so the avatar and PR pictures
# are drawn with Kitty graphics. macOS only. The terminal that runs this needs Screen Recording (to capture)
# and Accessibility (to press keys) in System Settings > Privacy & Security.
set -euo pipefail

cd "$(dirname "$0")/.."
out=docs/images
ghostty=/Applications/Ghostty.app
[[ $(uname) == Darwin && -d $ghostty ]] || { echo "needs macOS and $ghostty" >&2; exit 1; }
command -v cwebp >/dev/null || { echo "needs cwebp: brew install webp" >&2; exit 1; }
mkdir -p "$out"
go build -o bin/git-tui ./cmd/git-tui
bin=$PWD/bin/git-tui

pid=""
cleanup() { [[ -n $pid ]] && kill "$pid" 2>/dev/null || true; }
trap cleanup EXIT

# window prints the CGWindowID of the first normal window owned by pid, or nothing yet.
window() {
  osascript -l JavaScript -e "ObjC.import('CoreGraphics');
    var ws = ObjC.deepUnwrap(ObjC.castRefToObject(\$.CGWindowListCopyWindowInfo(\$.kCGWindowListOptionOnScreenOnly, \$.kCGNullWindowID)));
    var w = ws.find(w => w.kCGWindowOwnerPID == $1 && w.kCGWindowLayer == 0);
    w ? String(w.kCGWindowNumber) : ''"
}

# launch opens a fresh Ghostty, ignoring your own config so every run looks the same, cols×rows cells.
launch() {
  open -na "$ghostty" --args --config-default-files=false --font-size=13 --window-width="$1" --window-height="$2" \
    --window-save-state=never --confirm-close-surface=false --quit-after-last-window-closed=true \
    --title=git-tui --command="$(printf %q "$bin") -demo -profile full"
  pid=""
  for _ in $(seq 50); do
    pid=$(pgrep -n -x ghostty || true)
    [[ -n $pid && -n $(window "$pid") ]] && break
    sleep 0.2
  done
  [[ -n $pid && -n $(window "$pid") ]] || { echo "Ghostty window did not appear" >&2; exit 1; }
  osascript -e "tell application \"System Events\" to set frontmost of (first process whose unix id is $pid) to true"
  sleep 2 # first frame, demo data and the avatar
}

# front prints the process ID of the app in front, which receives System Events keystrokes.
front() {
  osascript -e 'tell application "System Events" to unix id of first process whose frontmost is true'
}

# keys types each argument into the window: a character, or f2, enter, esc, tab, shift-tab or space. Keystrokes go to whatever app is in front,
# so it brings the screenshot window forward first and stops rather than type into anything else.
keys() {
  for k in "$@"; do
    osascript -e "tell application \"System Events\" to set frontmost of (first process whose unix id is $pid) to true"
    if [[ $(front) != "$pid" ]]; then
      echo "The screenshot window is not in front, so no keys were sent. Leave the keyboard and mouse alone and run it again." >&2
      exit 1
    fi
    case $k in
      f2) osascript -e 'tell application "System Events" to key code 120' ;;
      enter) osascript -e 'tell application "System Events" to key code 36' ;;
      esc) osascript -e 'tell application "System Events" to key code 53' ;;
      tab) osascript -e 'tell application "System Events" to key code 48' ;;
      shift-tab) osascript -e 'tell application "System Events" to key code 48 using shift down' ;;
      space) osascript -e 'tell application "System Events" to keystroke " "' ;;
      *) osascript -e "tell application \"System Events\" to keystroke \"$k\"" ;;
    esac
    sleep 0.3
  done
  sleep 0.7 # let pictures load and the frame settle
}

# shot captures the window without its shadow, scaled to at most 1600 pixels wide, as lossy WebP: the pictures
# illustrate, and quality 90 keeps text crisp at about a fifth of a PNG's size, so the history grows slowly.
shot() {
  local png="$out/$1.png"
  screencapture -o -x -l"$(window "$pid")" "$png"
  sips -Z 1600 "$png" >/dev/null
  cwebp -quiet -q 90 "$png" -o "$out/$1.webp"
  rm "$png"
  echo "$out/$1.webp"
}

# finish closes the window by ending its process; no keystroke, so nothing can reach another app.
finish() {
  cleanup
  sleep 1
  pid=""
}

launch 140 40
shot overview
keys g p
shot pull-requests
keys v
shot detail
keys esc b
shot header-ascii
keys b
shot header-text
keys b b f2
shot light
finish

launch 80 24
keys g p
shot narrow
finish

# A fresh window for the rest, so they are in the dark theme with the full header.
launch 140 40
keys g c $(printf 'j %.0s' {1..11}) # the git-tui row, last in Connections
shot connections
keys g p d
shot diff
keys esc g d
shot deploys
keys shift-tab j tab g t enter j enter # backend, then its two newest tags
shot compare
keys g c a enter j space j space
shot add-repositories
finish
