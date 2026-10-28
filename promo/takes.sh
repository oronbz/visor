#!/bin/sh
# Cuts the recorded `visor demo` takes into public/takes. Takes keep their own
# clock, so src/timeline.json names moments in each take by its own seconds.
set -eu
keep="${KEEP:-$HOME/Developer/visor-launch/rec/keep}"
out="$(dirname "$0")/public/takes"
mkdir -p "$out"
cut() { # <take> <out> <seconds> [filter]
  ffmpeg -v error -y -i "$keep/$1" -t "$3" -vf "${4:-null}" -an -c:v libx264 -crf 14 -preset slow -g 15 -pix_fmt yuv420p "$out/$2"
}
cut keys-3.mp4 keys.mp4 26.1
cut mouse-7.mp4 mouse.mp4 26
# Claude Code's usage-limit banner shows until the prompt is typed.
cut configure-3.mp4 configure.mp4 40 "drawbox=x=280:y=962:w=1630:h=32:color=0x23222e:t=fill:enable='lt(t,5.75)'"
cut plugin-1.mp4 plugin.mp4 58
cp "$HOME/Developer/visor/docs/media/shai-mishali.jpg" "$out/shai.jpg"
