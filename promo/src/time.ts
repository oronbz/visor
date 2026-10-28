import { Easing, interpolate, useCurrentFrame } from 'remotion';
import timeline from './timeline.json';

export const T = timeline;
export const S = timeline.scenes as Record<keyof typeof timeline.scenes, [number, number]>;
export const C = timeline.cues;

/** Seconds since the start of the whole promo, from inside a scene. */
export const useAbs = (scene: keyof typeof S) => useCurrentFrame() / T.fps + S[scene][0];

export const ramp = (t: number, a: number, b: number, easing: (x: number) => number = Easing.inOut(Easing.cubic)) =>
  interpolate(t, [a, b], [0, 1], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp', easing });

export const span = (t: number, a: number, b: number, fadeIn = 0.25, fadeOut = 0.25) =>
  Math.min(ramp(t, a, a + fadeIn), 1 - ramp(t, b - fadeOut, b));
