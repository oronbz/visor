import React from 'react';
import { AbsoluteFill, Easing, Img, interpolate, Sequence, staticFile } from 'remotion';
import { Caption, Flash, Speed, WidgetBox } from './ui';
import { FULL, Take, View } from './Take';
import { color, font } from './theme';
import { C, ramp, S, T, useAbs } from './time';

// Where the demo's Widget borders sit in the keys take, in the take's pixels.
const K = 1920 / 1916;
const ROWS: [number, number, [string, number, number][]][] = [
  [114, 366, [['Clock', 221, 853], ['Calendar', 861, 1173], ['Now Playing', 1181, 1901]]],
  [384, 600, [['Reminders', 221, 589], ['Meetings', 597, 1029], ['Stocks', 1037, 1469], ['Weather', 1477, 1901]]],
  [618, 798, [['Jira', 221, 1061], ['Review requests', 1069, 1901]]],
  [816, 1032, [['Tasks', 221, 741], ['Notes', 749, 1269], ['System', 1277, 1901]]],
];
const WIDGETS = ROWS.flatMap(([top, bottom, cells]) =>
  cells.map(([title, l, r]) => [title, (l - 1) * K, (top - 1) * K, (r - l + 2) * K, (bottom - top + 2) * K] as const),
);

const PROMPT: View = { cx: 760, cy: 800, w: 1100 };
const REVIEW: View = { cx: 1450, cy: 706, w: 1000 };
const JIRA: View = { cx: 660, cy: 706, w: 1000 };
const HN: View = { cx: 1063, cy: 614, w: 1300 };

const Headline: React.FC<{ o: number; size?: number; children: React.ReactNode }> = ({ o, size = 104, children }) => (
  <AbsoluteFill style={{ alignItems: 'center', justifyContent: 'center', opacity: o }}>
    <div
      style={{
        fontFamily: font.sans,
        fontWeight: 800,
        fontSize: size,
        letterSpacing: -3,
        color: color.bright,
        textAlign: 'center',
        transform: `scale(${0.94 + 0.06 * o})`,
        textShadow: '0 10px 60px rgba(0,0,0,0.8)',
      }}
    >
      {children}
    </div>
  </AbsoluteFill>
);

export const Hook = () => {
  const t = useAbs('hook');
  const words = ['Your meetings.', 'Your PRs.', 'Your tickets.', 'Your tasks.'];
  const w = C.hookWords;
  const video = ramp(t, C.reveal, C.reveal + 0.5);
  return (
    <AbsoluteFill style={{ background: color.term }}>
      <Sequence from={Math.round(C.reveal * T.fps)}>
        <AbsoluteFill style={{ opacity: video }}>
          <Take src="keys.mp4" cuts={[{ from: 1.6, len: S.hook[1] - C.reveal }]} />
        </AbsoluteFill>
      </Sequence>
      {WIDGETS.map(([title, x, y, wd, h], i) => {
        const a = 0.05 + i * 0.19;
        const o = ramp(t, a, a + 0.18, Easing.out(Easing.cubic));
        const lit = 1 - ramp(t, a + 0.1, a + 0.6);
        return (
          <WidgetBox
            key={title}
            title={title}
            x={x}
            y={y}
            w={wd}
            h={h}
            border={lit > 0.05 ? color.accent : color.line}
            titleColor={lit > 0.05 ? color.accent : color.fg}
            style={{ opacity: o * (1 - video), transform: `scale(${0.96 + 0.04 * o})` }}
          />
        );
      })}
      <AbsoluteFill style={{ background: color.term, opacity: 0.55 * (1 - ramp(t, C.reveal + 0.3, C.reveal + 1.2)) }} />
      {words.map((word, i) => {
        const a = w[i];
        const b = i + 1 < w.length ? w[i + 1] : C.reveal;
        const o = t >= a && t < b ? ramp(t, a, a + 0.12, Easing.out(Easing.back(2))) : 0;
        return o > 0 ? (
          <Headline key={word} o={o}>
            {word}
          </Headline>
        ) : null;
      })}
      <AbsoluteFill style={{ background: `rgba(19,23,29,${0.55 * ramp(t, C.reveal + 0.2, C.reveal + 0.6)})` }} />
      <Headline o={ramp(t, C.reveal + 0.2, C.reveal + 0.5, Easing.out(Easing.back(1.6)))}>
        All on one screen.
        <div style={{ fontSize: 52, fontWeight: 500, letterSpacing: -1, color: color.fg, marginTop: 18 }}>In your terminal.</div>
      </Headline>
    </AbsoluteFill>
  );
};

export const Title = () => {
  const t = useAbs('title');
  const a = S.title[0];
  const pop = ramp(t, a, a + 0.35, Easing.out(Easing.back(2.2)));
  const tag = ramp(t, a + 0.45, a + 0.8, Easing.out(Easing.cubic));
  const blink = Math.floor((t - a) * 2.4) % 2 === 0;
  return (
    <AbsoluteFill>
      <AbsoluteFill style={{ filter: 'blur(14px) brightness(0.45)', transform: `scale(${1.08 - 0.03 * ramp(t, a, S.title[1])})` }}>
        <Take src="keys.mp4" cuts={[{ from: 3.6, len: S.title[1] - a }]} />
      </AbsoluteFill>
      <Flash t={t} at={a} strength={0.5} />
      <AbsoluteFill style={{ alignItems: 'center', justifyContent: 'center' }}>
        <div style={{ display: 'flex', alignItems: 'center', transform: `scale(${0.6 + 0.4 * pop})`, opacity: pop }}>
          <span style={{ fontFamily: font.mono, fontWeight: 800, fontSize: 260, letterSpacing: -10, color: color.bright }}>visor</span>
          <span style={{ width: 70, height: 200, marginLeft: 14, background: blink ? color.accent : 'transparent', borderRadius: 6 }} />
        </div>
        <div style={{ marginTop: 10, opacity: tag, transform: `translateY(${(1 - tag) * 24}px)`, fontFamily: font.sans, fontWeight: 500, fontSize: 56, color: color.fg }}>
          A heads-up display for your terminal.
        </div>
      </AbsoluteFill>
    </AbsoluteFill>
  );
};

export const Open = () => {
  const t = useAbs('open');
  return (
    <AbsoluteFill>
      <Take src="keys.mp4" cuts={[{ from: 0.3, len: 3 }]} />
      <Flash t={t} at={C.keyOpen} strength={0.25} />
      <Caption t={t} a={S.open[0] + 0.15} b={S.open[1]} title="herdr">
        One key opens it over any tab.
      </Caption>
    </AbsoluteFill>
  );
};

export const Approve = () => {
  const t = useAbs('approve');
  return (
    <AbsoluteFill>
      <Take src="keys.mp4" cuts={[{ from: 5.4, len: 3.5, cam: [[0, FULL], [0.7, REVIEW]] }]} />
      <Caption t={t} a={S.approve[0] + 0.6} b={S.approve[1]} title="Review requests">
        Approve a PR without leaving.
      </Caption>
    </AbsoluteFill>
  );
};

export const Jira = () => {
  const t = useAbs('jira');
  return (
    <AbsoluteFill>
      <Take src="keys.mp4" cuts={[{ from: 11.9, len: 3.5, cam: [[0, REVIEW], [0.5, JIRA]] }]} />
      <Caption t={t} a={S.jira[0] + 0.4} b={S.jira[1]} title="Jira">
        Move a ticket to review.
      </Caption>
    </AbsoluteFill>
  );
};

export const Mouse = () => {
  const t = useAbs('mouse');
  const near: View = { cx: 1060, cy: 560, w: 1700 };
  return (
    <AbsoluteFill>
      <Take
        src="mouse.mp4"
        cuts={[
          { from: 6.0, len: 2.75, rate: 1.6, cam: [[0, JIRA], [0.4, near]] },
          { from: 21.8, len: 2.25, rate: 1.6, cam: [[0, near], [1.2, REVIEW]] },
        ]}
      />
      <Caption t={t} a={S.mouse[0] + 0.5} b={S.mouse[1]} title="mouse">
        Drag it. Resize it. Right-click it.
      </Caption>
    </AbsoluteFill>
  );
};

export const Ask = () => {
  const t = useAbs('ask');
  const [a, b] = S.ask;
  const one = ramp(t, a + 0.05, a + 0.3, Easing.out(Easing.back(1.8)));
  const two = ramp(t, a + 0.75, a + 1.0, Easing.out(Easing.cubic));
  const agents = ['Claude Code', 'Codex', 'Cursor'];
  return (
    <AbsoluteFill style={{ background: color.term }}>
      <Headline o={one * (1 - ramp(t, b - 0.15, b))} size={124}>
        Now ask your agent.
        <div style={{ display: 'flex', gap: 22, justifyContent: 'center', marginTop: 34, opacity: two }}>
          {agents.map((name, i) => (
            <span
              key={name}
              style={{
                fontFamily: font.mono,
                fontWeight: 400,
                fontSize: 36,
                letterSpacing: 0,
                color: color.accent,
                border: `2px solid ${color.line}`,
                borderRadius: 999,
                padding: '8px 26px',
                opacity: ramp(t, a + 0.75 + i * 0.12, a + 0.95 + i * 0.12),
              }}
            >
              {name}
            </span>
          ))}
        </div>
      </Headline>
    </AbsoluteFill>
  );
};

/** An agent take: the prompt typed up close, the wait fast-forwarded, then visor changing live. */
const AgentTake: React.FC<{
  scene: 'configure' | 'plugin';
  src: string;
  typing: [number, number];
  wait: number;
  payoff: number;
  payoffAt: number;
  zoomTo?: View;
  caption: string;
  title: string;
}> = ({ scene, src, typing, wait, payoff, payoffAt, zoomTo, caption, title }) => {
  const t = useAbs(scene);
  const [a, b] = S[scene];
  const typeLen = 2;
  const waitLen = 2;
  const typeRate = (typing[1] - typing[0]) / typeLen;
  const waitRate = (payoff - wait) / waitLen;
  const hit = payoffAt - (a + typeLen + waitLen);
  const end: [number, View][] = zoomTo ? [[0, FULL], [hit + 0.25, FULL], [hit + 1.1, zoomTo]] : [[0, FULL]];
  return (
    <AbsoluteFill>
      <Take
        src={src}
        cuts={[
          { from: typing[0], len: typeLen, rate: typeRate, cam: [[0, PROMPT]] },
          { from: wait, len: waitLen, rate: waitRate, cam: [[0, PROMPT], [1.2, FULL]] },
          { from: payoff, len: b - a - typeLen - waitLen, cam: end },
        ]}
      />
      <Speed t={t} a={a} b={a + typeLen} rate={typeRate} />
      <Speed t={t} a={a + typeLen} b={a + typeLen + waitLen} rate={waitRate} />
      <Flash t={t} at={payoffAt} strength={0.45} />
      <Caption t={t} a={payoffAt + 0.35} b={b} title={title} y={zoomTo ? 880 : 860}>
        {caption}
      </Caption>
    </AbsoluteFill>
  );
};

// The takes' own seconds: when the prompt is typed, when Claude starts, and when visor changes.
export const Configure = () => (
  <AgentTake
    scene="configure"
    src="configure.mp4"
    typing={[2.0, 11.0]}
    wait={11.0}
    payoff={30.89}
    payoffAt={C.configureFlip}
    caption="New colours and a new Widget, live."
    title="visor-configure"
  />
);

export const Plugin = () => (
  <AgentTake
    scene="plugin"
    src="plugin.mp4"
    typing={[2.0, 11.0]}
    wait={11.0}
    payoff={49.31}
    payoffAt={C.pluginAppears}
    zoomTo={HN}
    caption="A whole new Plugin in 43 seconds."
    title="visor-make-plugin"
  />
);

export const Shai = () => {
  const t = useAbs('shai');
  const [a, b] = S.shai;
  const k = ramp(t, a, b, Easing.linear);
  const o = ramp(t, a, a + 0.3);
  return (
    <AbsoluteFill style={{ background: color.term, alignItems: 'center', justifyContent: 'center' }}>
      <Img
        src={staticFile('takes/shai.jpg')}
        style={{
          height: 900,
          borderRadius: 18,
          opacity: o,
          boxShadow: '0 30px 120px rgba(0,0,0,0.6)',
          transform: `translateY(-50px) scale(${1.0 + 0.06 * k})`,
        }}
      />
      <Caption t={t} a={a + 0.4} b={b} title="Shai Mishali's visor" y={890}>
        Spotify and his 3D printer, as Plugins.
      </Caption>
    </AbsoluteFill>
  );
};

export const End = () => {
  const t = useAbs('end');
  const [a, b] = S.end;
  const pop = ramp(t, a, a + 0.35, Easing.out(Easing.back(2)));
  const box = ramp(t, a + 0.5, a + 0.9, Easing.out(Easing.cubic));
  const { start, perChar, lines } = C.endTyping;
  let cursor = start;
  const typed = lines.map((line) => {
    const n = Math.max(0, Math.min(line.length, Math.floor((t - cursor) / perChar)));
    const shown = t >= cursor ? line.slice(0, n) : null;
    cursor += line.length * perChar + 0.45;
    return shown;
  });
  const last = typed.reduce((k, l, i) => (l === null ? k : i), -1);
  const url = ramp(t, cursor - 0.2, cursor + 0.2);
  const out = 1 - ramp(t, b - 0.6, b);
  const blink = Math.floor((t - a) * 2.4) % 2 === 0;
  return (
    <AbsoluteFill style={{ background: color.term, alignItems: 'center', justifyContent: 'center', opacity: out }}>
      <Flash t={t} at={a} strength={0.35} />
      <div style={{ fontFamily: font.mono, fontWeight: 800, fontSize: 170, letterSpacing: -7, color: color.bright, transform: `scale(${0.7 + 0.3 * pop})`, opacity: pop, marginTop: -60 }}>
        visor
      </div>
      <div style={{ position: 'relative', width: 1000, height: 190, marginTop: 50, opacity: box, transform: `translateY(${(1 - box) * 30}px)` }}>
        <WidgetBox title="terminal" x={0} y={0} w={1000} h={190} border={color.accent} titleColor={color.accent} size={26} fill={color.bg}>
          <div style={{ position: 'absolute', left: 44, top: 38, fontFamily: font.mono, fontSize: 44, lineHeight: '62px', color: color.bright }}>
            {typed.map((line, i) =>
              line === null ? null : (
                <div key={i}>
                  <span style={{ color: color.ok }}>$ </span>
                  {line}
                  {i === last && blink ? <span style={{ color: color.accent }}>▌</span> : null}
                </div>
              ),
            )}
          </div>
        </WidgetBox>
      </div>
      <div style={{ marginTop: 56, opacity: url, fontFamily: font.sans, fontWeight: 700, fontSize: 52, color: color.fg }}>
        github.com/oronbz/visor
      </div>
      <div style={{ marginTop: 14, opacity: url, fontFamily: font.sans, fontWeight: 500, fontSize: 32, color: color.dim }}>
        Free and open source · macOS, Linux and Windows
      </div>
    </AbsoluteFill>
  );
};
