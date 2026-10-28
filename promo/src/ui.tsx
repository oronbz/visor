import React from 'react';
import { AbsoluteFill, Easing, interpolate } from 'remotion';
import { color, font } from './theme';
import { ramp } from './time';

/** A box drawn the way visor draws a Widget: a rounded border with its title cut into the top edge. */
export const WidgetBox: React.FC<{
  title: string;
  x: number;
  y: number;
  w: number;
  h: number;
  border?: string;
  titleColor?: string;
  fill?: string;
  size?: number;
  style?: React.CSSProperties;
  children?: React.ReactNode;
}> = ({ title, x, y, w, h, border = color.line, titleColor = color.fg, fill = 'transparent', size = 22, style, children }) => (
  <div style={{ position: 'absolute', left: x, top: y, width: w, height: h, ...style }}>
    <div style={{ position: 'absolute', inset: 0, border: `2px solid ${border}`, borderRadius: 10, background: fill }} />
    <div
      style={{
        position: 'absolute',
        left: 18,
        top: -size * 0.68,
        padding: '0 8px',
        background: color.term,
        color: titleColor,
        fontFamily: font.mono,
        fontSize: size,
        lineHeight: `${size * 1.3}px`,
        whiteSpace: 'nowrap',
      }}
    >
      {title}
    </div>
    {children}
  </div>
);

/** A lower-third caption in a Widget box that slides up into place. */
export const Caption: React.FC<{ t: number; a: number; b: number; title?: string; children: React.ReactNode; y?: number }> = ({
  t,
  a,
  b,
  title = 'visor',
  children,
  y = 860,
}) => {
  const o = Math.min(ramp(t, a, a + 0.3, Easing.out(Easing.cubic)), 1 - ramp(t, b - 0.2, b));
  if (o <= 0) return null;
  return (
    <AbsoluteFill style={{ alignItems: 'center', pointerEvents: 'none' }}>
      <div
        style={{
          position: 'absolute',
          top: y + (1 - o) * 40,
          opacity: o,
          padding: '30px 52px 26px',
          background: 'rgba(19,23,29,0.92)',
          border: `2px solid ${color.accent}`,
          borderRadius: 16,
          boxShadow: '0 24px 80px rgba(0,0,0,0.55)',
          color: color.bright,
          fontFamily: font.sans,
          fontWeight: 700,
          fontSize: 58,
          letterSpacing: -1,
          whiteSpace: 'nowrap',
        }}
      >
        <div
          style={{
            position: 'absolute',
            left: 26,
            top: -17,
            padding: '0 10px',
            background: color.term,
            color: color.accent,
            fontFamily: font.mono,
            fontWeight: 400,
            fontSize: 24,
            letterSpacing: 0,
            borderRadius: 6,
          }}
        >
          {title}
        </div>
        {children}
      </div>
    </AbsoluteFill>
  );
};

/** How much faster than real time a stretch plays, shown while it does. */
export const Speed: React.FC<{ t: number; a: number; b: number; rate: number }> = ({ t, a, b, rate }) => {
  const o = Math.min(ramp(t, a, a + 0.15), 1 - ramp(t, b - 0.15, b));
  if (o <= 0) return null;
  return (
    <div
      style={{
        position: 'absolute',
        right: 48,
        top: 44,
        opacity: o,
        padding: '10px 22px',
        borderRadius: 999,
        background: color.accent,
        color: color.bg,
        fontFamily: font.mono,
        fontWeight: 800,
        fontSize: 34,
      }}
    >
      ▶▶ {rate < 3 ? rate.toFixed(1).replace(/\.0$/, '') : Math.round(rate)}×
    </div>
  );
};

/** A white flash that peaks at `at`. */
export const Flash: React.FC<{ t: number; at: number; strength?: number }> = ({ t, at, strength = 0.7 }) => {
  const o = interpolate(t, [at - 0.04, at, at + 0.45], [0, strength, 0], { extrapolateLeft: 'clamp', extrapolateRight: 'clamp' });
  return o > 0 ? <AbsoluteFill style={{ background: '#fff', opacity: o, mixBlendMode: 'screen' }} /> : null;
};

export const Vignette = () => (
  <AbsoluteFill style={{ pointerEvents: 'none', background: 'radial-gradient(ellipse 80% 75% at 50% 50%, transparent 60%, rgba(0,0,0,0.45) 100%)' }} />
);
