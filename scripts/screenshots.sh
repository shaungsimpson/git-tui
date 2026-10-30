#!/usr/bin/env bash
# Captures README screenshots of `git-tui -demo` in a real Ghostty window, so the avatar and PR pictures
# are drawn with Kitty graphics. macOS only. The terminal that runs this needs Screen Recording (to capture)
# and Accessibility (to press keys) in System Settings > Privacy & Security.
set -euo pipefail

cd "$(dirname "$0")/.."
out=docs/images
ghostty=/Applications/Ghostty.app
[[ $(uname) == Darwin && -d $ghostty ]] || { echo "needs macOS and $ghostty" >&2; exit 1; }
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

# keys types each argument into the window: a character, or f2 / ctrl+c.
keys() {
  for k in "$@"; do
    case $k in
      f2) osascript -e 'tell application "System Events" to key code 120' ;;
      ctrl+c) osascript -e 'tell application "System Events" to keystroke "c" using control down' ;;
      *) osascript -e "tell application \"System Events\" to keystroke \"$k\"" ;;
    esac
    sleep 0.3
  done
  sleep 0.7 # let pictures load and the frame settle
}

# shot captures the window without its shadow, scaled to at most 1600 pixels wide.
shot() {
  screencapture -o -x -l"$(window "$pid")" "$out/$1.png"
  sips -Z 1600 "$out/$1.png" >/dev/null
  echo "$out/$1.png"
}

finish() {
  keys ctrl+c
  sleep 1
  cleanup
  pid=""
}

launch 140 40
shot overview
keys g p
shot pull-requests
keys b
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
