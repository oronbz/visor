import React from 'react';
import { AbsoluteFill, Easing, interpolate, OffthreadVideo, Sequence, staticFile, useCurrentFrame } from 'remotion';
import { T } from './time';

/** A point the camera looks at, in composition pixels, and how wide it sees. */
export type View = { cx: number; cy: number; w: number };
export const FULL: View = { cx: 960, cy: 540, w: 1920 };

/** One stretch of a take: `from` is in the take's own seconds, `len` in the promo's. */
export type Cut = { from: number; len: number; rate?: number; cam?: [number, View][] };

const at = (t: number, keys: [number, View][]): View => {
  if (keys.length === 1 || t <= keys[0][0]) return keys[0][1];
  for (let i = 1; i < keys.length; i++) {
    const [t0, a] = keys[i - 1];
    const [t1, b] = keys[i];
    if (t <= t1) {
      const k = interpolate(t, [t0, t1], [0, 1], { easing: Easing.inOut(Easing.cubic) });
      return { cx: a.cx + (b.cx - a.cx) * k, cy: a.cy + (b.cy - a.cy) * k, w: a.w + (b.w - a.w) * k };
    }
  }
  return keys[keys.length - 1][1];
};

const Shot: React.FC<{ src: string; cut: Cut }> = ({ src, cut }) => {
  const t = useCurrentFrame() / T.fps;
  const v = at(t, cut.cam ?? [[0, FULL]]);
  const s = 1920 / v.w;
  const h = v.w * (9 / 16);
  const x = Math.min(Math.max(v.cx - v.w / 2, 0), 1920 - v.w);
  const y = Math.min(Math.max(v.cy - h / 2, 0), 1080 - h);
  return (
    <AbsoluteFill style={{ overflow: 'hidden', background: '#23222e' }}>
      <div style={{ position: 'absolute', width: 1920, height: 1080, transformOrigin: '0 0', transform: `scale(${s}) translate(${-x}px, ${-y}px)` }}>
        <OffthreadVideo
          src={staticFile(`takes/${src}`)}
          startFrom={Math.round(cut.from * T.fps)}
          playbackRate={cut.rate ?? 1}
          muted
          style={{ width: 1920, height: 1080 }}
        />
      </div>
    </AbsoluteFill>
  );
};

/** Plays a take's cuts back to back. */
export const Take: React.FC<{ src: string; cuts: Cut[] }> = ({ src, cuts }) => {
  let from = 0;
  return (
    <AbsoluteFill>
      {cuts.map((cut, i) => {
        const start = from;
        from += Math.round(cut.len * T.fps);
        return (
          <Sequence key={i} from={start} durationInFrames={Math.round(cut.len * T.fps)}>
            <Shot src={src} cut={cut} />
          </Sequence>
        );
      })}
    </AbsoluteFill>
  );
};
