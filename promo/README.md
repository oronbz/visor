# Promo video

A 50-second promo for visor, built with [Remotion](https://www.remotion.dev). The footage is real: `visor demo` recorded in herdr and Ghostty, with Claude Code configuring visor and writing a Plugin. The captions, the outlines and the soundtrack are generated; `audio/build.mjs` synthesizes the music and sound effects.

```sh
npm install
npm run takes    # cuts the recorded takes into public/takes
npm run studio   # live preview
npm run render   # writes out/visor-promo.mp4
```

`src/timeline.json` holds every scene boundary and cue, and both the picture and the soundtrack read it. Each take keeps its own clock, so the scenes name moments in a take by its own seconds. `takes.sh` reads the takes from `~/Developer/visor-launch/rec/keep`, or `$KEEP`.
