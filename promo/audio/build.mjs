import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const T = JSON.parse(fs.readFileSync(path.join(here, '../src/timeline.json'), 'utf8'));
const C = T.cues;
const S = T.scenes;
const SR = 48000;
const N = Math.ceil(T.duration * SR);
const BEAT = 60 / T.bpm;
const BAR = BEAT * 4;

let seed = 2026;
const rnd = () => {
  seed = (seed + 0x6d2b79f5) | 0;
  let t = Math.imul(seed ^ (seed >>> 15), 1 | seed);
  t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
  return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
};
const rn = () => rnd() * 2 - 1;

const NOTE = { C: 0, 'C#': 1, D: 2, 'D#': 3, E: 4, F: 5, 'F#': 6, G: 7, 'G#': 8, A: 9, 'A#': 10, B: 11 };
const hz = (name) => {
  const m = /^([A-G]#?)(-?\d)$/.exec(name);
  return 440 * 2 ** ((NOTE[m[1]] + (Number(m[2]) + 1) * 12 - 69) / 12);
};

const bus = () => [new Float32Array(N), new Float32Array(N)];
const music = bus();
const drums = bus();
const sfx = bus();
const send = bus();

function add(b, t, sig, gain = 1, pan = 0, wet = 0) {
  const s0 = Math.round(t * SR);
  const gl = gain * Math.cos(((pan + 1) * Math.PI) / 4);
  const gr = gain * Math.sin(((pan + 1) * Math.PI) / 4);
  for (let i = 0; i < sig.length; i++) {
    const j = s0 + i;
    if (j < 0 || j >= N) continue;
    b[0][j] += sig[i] * gl;
    b[1][j] += sig[i] * gr;
    if (wet) {
      send[0][j] += sig[i] * gl * wet;
      send[1][j] += sig[i] * gr * wet;
    }
  }
}

const len = (d) => Math.max(1, Math.floor(d * SR));
const white = (d) => Float32Array.from({ length: len(d) }, rn);

function coeffs(type, f, q) {
  const w = (2 * Math.PI * Math.min(f, SR * 0.45)) / SR;
  const cs = Math.cos(w);
  const a = Math.sin(w) / (2 * q);
  let b0, b1, b2;
  if (type === 'lp') [b0, b1, b2] = [(1 - cs) / 2, 1 - cs, (1 - cs) / 2];
  else if (type === 'hp') [b0, b1, b2] = [(1 + cs) / 2, -(1 + cs), (1 + cs) / 2];
  else [b0, b1, b2] = [a, 0, -a];
  const a0 = 1 + a;
  return [b0 / a0, b1 / a0, b2 / a0, (-2 * cs) / a0, (1 - a) / a0];
}

function filt(sig, type, f, q = 0.707) {
  const fAt = typeof f === 'function' ? f : () => f;
  let c = coeffs(type, fAt(0), q);
  let x1 = 0, x2 = 0, y1 = 0, y2 = 0;
  for (let i = 0; i < sig.length; i++) {
    if (typeof f === 'function' && i % 32 === 0) c = coeffs(type, fAt(i / SR), q);
    const x = sig[i];
    const y = c[0] * x + c[1] * x1 + c[2] * x2 - c[3] * y1 - c[4] * y2;
    x2 = x1; x1 = x; y2 = y1; y1 = y;
    sig[i] = y;
  }
  return sig;
}

function env(sig, fn) {
  for (let i = 0; i < sig.length; i++) sig[i] *= fn(i / SR, sig.length / SR);
  return sig;
}
const expDecay = (tau) => (t) => Math.exp(-t / tau);
const ar = (a, r) => (t, d) => Math.min(1, t / a) * Math.min(1, Math.max(0, (d - t) / r));

function polyblep(t, dt) {
  if (t < dt) { t /= dt; return t + t - t * t - 1; }
  if (t > 1 - dt) { t = (t - 1) / dt; return t * t + t + t + 1; }
  return 0;
}

function saw(f, dur, detunes = [0]) {
  const o = new Float32Array(len(dur));
  for (const det of detunes) {
    let ph = rnd();
    const inc = (f * (1 + det)) / SR;
    for (let i = 0; i < o.length; i++) {
      ph += inc;
      if (ph >= 1) ph -= 1;
      o[i] += (2 * ph - 1 - polyblep(ph, inc)) / detunes.length;
    }
  }
  return o;
}

// Sections: the beat plays in the drops, the outro holds one chord.
const DROPS = [[S.title[0], S.ask[0]], [S.configure[0], S.end[0]]];
const inDrop = (t) => DROPS.some(([a, b]) => t >= a && t < b);
const CHORDS = [
  ['A1', ['A3', 'C4', 'E4']],
  ['F1', ['F3', 'A3', 'C4']],
  ['C2', ['G3', 'C4', 'E4']],
  ['G1', ['G3', 'B3', 'D4']],
];
const up = (name) => name.replace(/\d/, (d) => String(Number(d) + 1));
const chordAt = (t) => CHORDS[Math.floor(t / BAR + 1e-6) % CHORDS.length];

/** The duck the kick puts on the music, as a sidechain would. */
const pumpAt = (t) => {
  if (!inDrop(t)) return 1;
  const p = (t % BEAT) / BEAT;
  return 0.45 + 0.55 * Math.min(1, p / 0.4) ** 1.5;
};

function kick(t0, amp) {
  const d = 0.42;
  const o = new Float32Array(len(d));
  let ph = 0;
  for (let i = 0; i < o.length; i++) {
    const t = i / SR;
    ph += (45 + 120 * Math.exp(-t / 0.035)) / SR;
    o[i] = Math.sin(2 * Math.PI * ph) * Math.exp(-t / 0.16) + (i < 90 ? rn() * 0.4 * (1 - i / 90) : 0);
  }
  add(drums, t0, o, amp);
}

function clap(t0, amp) {
  const o = filt(white(0.25), 'bp', 1500, 1.1);
  env(o, (t) => (t < 0.03 ? (Math.floor(t / 0.009) % 2 === 0 ? 1 : 0.4) : Math.exp(-(t - 0.03) / 0.07)));
  add(drums, t0, o, amp, 0, 0.15);
}

function hat(t0, amp, open = false) {
  const o = filt(white(open ? 0.25 : 0.05), 'hp', 8000);
  env(o, expDecay(open ? 0.08 : 0.014));
  add(drums, t0, o, amp, 0.25);
}

function bass(t0, name, dur, amp) {
  const f = hz(name);
  const o = saw(f, dur, [-0.004, 0.004]);
  const sub = new Float32Array(o.length);
  for (let i = 0; i < sub.length; i++) sub[i] = Math.sin((2 * Math.PI * f * i) / SR) * 0.8;
  filt(o, 'lp', (t) => 180 + 900 * Math.exp(-t / 0.08), 1.4);
  for (let i = 0; i < o.length; i++) o[i] += sub[i];
  env(o, ar(0.004, 0.03));
  add(music, t0, o, amp);
}

function pad(t0, dur, notes, amp, cutoff) {
  for (const [k, name] of notes.entries()) {
    const o = saw(hz(name), dur, [-0.011, -0.005, 0, 0.006, 0.012]);
    filt(o, 'lp', cutoff, 0.9);
    env(o, ar(0.06, 0.12));
    add(music, t0, o, amp, (k - 1) * 0.5, 0.25);
  }
}

function pluck(t0, name, amp, cutoff, pan) {
  const o = saw(hz(name), 0.24, [-0.003, 0.003]);
  filt(o, 'lp', (t) => cutoff * (0.25 + Math.exp(-t / 0.05)), 1.4);
  env(o, (t, d) => Math.exp(-t / 0.07) * Math.min(1, (d - t) / 0.02));
  add(music, t0, o, amp, pan, 0.2);
}

function riser(t0, t1, amp) {
  const d = t1 - t0;
  const o = filt(white(d), 'bp', (t) => 300 * 30 ** (t / d), 3);
  env(o, (t) => (t / d) ** 2);
  add(sfx, t0, o, amp, 0, 0.4);
}

function impact(t0, amp) {
  const d = 1.6;
  const o = new Float32Array(len(d));
  let ph = 0;
  for (let i = 0; i < o.length; i++) {
    const t = i / SR;
    ph += (38 + 90 * Math.exp(-t / 0.08)) / SR;
    o[i] = Math.sin(2 * Math.PI * ph) * Math.exp(-t / 0.5);
  }
  add(sfx, t0, o, amp);
  const crash = filt(white(2.2), 'hp', 4500);
  env(crash, expDecay(0.7));
  add(sfx, t0, crash, amp * 0.28, 0, 0.6);
}

function whoosh(t0, amp) {
  const d = 0.5;
  const o = filt(white(d), 'bp', (t) => 400 + 5000 * Math.sin((Math.PI * t) / d), 1.5);
  env(o, (t) => Math.sin((Math.PI * t) / d) ** 2);
  add(sfx, t0 - d * 0.6, o, amp, 0, 0.3);
}

function key(t0, amp, pan = 0) {
  const o = filt(white(0.03), 'bp', 2200 + rnd() * 1400, 3);
  env(o, expDecay(0.006));
  const thock = new Float32Array(len(0.04));
  for (let i = 0; i < thock.length; i++) thock[i] = Math.sin((2 * Math.PI * 180 * i) / SR) * Math.exp(-i / SR / 0.008);
  add(sfx, t0, o, amp, pan, 0.1);
  add(sfx, t0, thock, amp * 0.5, pan);
}

function typing(t0, t1, perSec, amp) {
  for (let t = t0; t < t1; t += (1 / perSec) * (0.7 + rnd() * 0.6)) key(t, amp * (0.7 + rnd() * 0.3), rn() * 0.3);
}

// Music: one chord per bar, an 8th-note arpeggio over it, bass on the off-beats.
const ARP = [0, 1, 2, 3, 2, 1, 0, 1];
for (let k = 0; k * BAR < S.end[0] - 1e-6; k++) {
  const t0 = k * BAR;
  const [root, notes] = chordAt(t0);
  const intro = t0 < S.title[0];
  const ask = t0 >= S.ask[0] && t0 < S.configure[0];
  const open = intro ? t0 / S.title[0] : 1;
  pad(t0, BAR, notes, ask ? 0.1 : 0.075, ask ? 900 : 700 + 1600 * open);
  if (ask) continue;
  const tones = [...notes, up(notes[0])];
  for (let s8 = 0; s8 < 8; s8++) {
    const t = t0 + (s8 * BEAT) / 2;
    pluck(t, up(tones[ARP[s8]]), 0.05 + 0.03 * open, 1200 + 2400 * open, s8 % 2 ? 0.3 : -0.3);
    if (inDrop(t) && s8 % 2 === 1) bass(t, root, BEAT / 2 - 0.02, 0.36);
  }
}

// Drums.
for (let t = 0; t < S.end[0] - 1e-6; t += BEAT / 2) {
  const s8 = Math.round(t / (BEAT / 2)) % 8;
  if (inDrop(t)) {
    if (s8 % 2 === 0) kick(t, 0.95);
    if (s8 === 2 || s8 === 6) clap(t, 0.45);
    if (s8 % 2 === 1) hat(t, 0.16, t >= S.configure[0]);
  } else if (t >= C.reveal && t < S.title[0] && s8 % 2 === 0) {
    kick(t, 0.75);
  }
}
for (const t of C.hookWords) clap(t, 0.3);
for (let k = 0; k < 8; k++) clap(S.configure[0] - 1 + k / 8, 0.12 + 0.25 * (k / 8));

// Sections.
riser(C.reveal, S.title[0], 0.35);
riser(S.configure[0] - 2.5, S.configure[0], 0.4);
for (const t of [S.title[0], S.configure[0], S.end[0]]) impact(t, 0.7);
for (const t of [C.configureFlip, C.pluginAppears]) impact(t, 0.45);
for (const t of [S.open[0], S.mouse[0], S.ask[0], S.plugin[0], S.shai[0]]) whoosh(t, 0.15);
const [, last] = CHORDS[0];
pad(S.end[0], S.end[1] - S.end[0], [...last, up(last[0])], 0.09, 2000);
bass(S.end[0], 'A1', 3, 0.35);
for (let k = 0; k < 6; k++) pluck(S.end[0] + k * BEAT, up([...last, up(last[0])][ARP[k]]), 0.06 * (1 - k / 7), 3000, k % 2 ? 0.3 : -0.3);

// Sound effects.
key(C.keyOpen, 0.6);
for (const t of C.confirm) key(t, 0.55);
for (const t of C.transition) key(t, 0.55);
for (const t of C.drags) key(t, 0.35);
typing(...C.configureTyping, 15, 0.26);
typing(...C.pluginTyping, 15, 0.26);
{
  let t = C.endTyping.start;
  for (const line of C.endTyping.lines) {
    for (let i = 0; i < line.length; i++) key(t + i * C.endTyping.perChar, 0.3, rn() * 0.3);
    t += line.length * C.endTyping.perChar + 0.45;
    key(t - 0.4, 0.45);
  }
}

// Sidechain the music under the kick.
for (let i = 0; i < N; i++) {
  const g = pumpAt(i / SR);
  music[0][i] *= g;
  music[1][i] *= g;
}

function freeverb(inL, inR, { room = 0.72, damp = 0.45 } = {}) {
  const sc = SR / 44100;
  const combs = [1116, 1188, 1277, 1356, 1422, 1491, 1557, 1617];
  const aps = [556, 441, 341, 225];
  const run = (spread) => {
    const cb = combs.map((c) => ({ b: new Float32Array(Math.round((c + spread) * sc)), i: 0, f: 0 }));
    const ab = aps.map((a) => ({ b: new Float32Array(Math.round((a + spread) * sc)), i: 0 }));
    const out = new Float32Array(N);
    for (let n = 0; n < N; n++) {
      const x = (inL[n] + inR[n]) * 0.015;
      let y = 0;
      for (const c of cb) {
        const o = c.b[c.i];
        c.f = o * (1 - damp) + c.f * damp;
        c.b[c.i] = x + c.f * room;
        c.i = (c.i + 1) % c.b.length;
        y += o;
      }
      for (const a of ab) {
        const o = a.b[a.i];
        const z = -y + o;
        a.b[a.i] = y + o * 0.5;
        a.i = (a.i + 1) % a.b.length;
        y = z;
      }
      out[n] = y;
    }
    return out;
  };
  return [run(0), run(23)];
}

for (const ch of [0, 1]) filt(music[ch], 'hp', 35);
const [vl, vr] = freeverb(send[0], send[1]);
const G = { music: 0.9, drums: 1.0, sfx: 0.85, verb: 0.8 };
const mix = bus();
for (let i = 0; i < N; i++) {
  mix[0][i] = music[0][i] * G.music + drums[0][i] * G.drums + sfx[0][i] * G.sfx + vl[i] * G.verb;
  mix[1][i] = music[1][i] * G.music + drums[1][i] * G.drums + sfx[1][i] * G.sfx + vr[i] * G.verb;
}
const tail = len(1.2);
for (let i = 0; i < tail; i++) {
  mix[0][N - 1 - i] *= i / tail;
  mix[1][N - 1 - i] *= i / tail;
}

let peak = 0;
for (const ch of mix) for (let i = 0; i < N; i++) peak = Math.max(peak, Math.abs(ch[i]));
const drive = 1.4 / peak;
let outPeak = 0;
for (const ch of mix) for (let i = 0; i < N; i++) {
  ch[i] = Math.tanh(ch[i] * drive);
  outPeak = Math.max(outPeak, Math.abs(ch[i]));
}
const norm = 0.93 / outPeak;
let ss = 0;
for (const ch of mix) for (let i = 0; i < N; i++) ss += (ch[i] * norm) ** 2;
console.log(`master rms ${(20 * Math.log10(Math.sqrt(ss / (2 * N)))).toFixed(1)} dBFS`);

const out = Buffer.alloc(44 + N * 4);
out.write('RIFF', 0); out.writeUInt32LE(36 + N * 4, 4); out.write('WAVE', 8);
out.write('fmt ', 12); out.writeUInt32LE(16, 16); out.writeUInt16LE(1, 20); out.writeUInt16LE(2, 22);
out.writeUInt32LE(SR, 24); out.writeUInt32LE(SR * 4, 28); out.writeUInt16LE(4, 32); out.writeUInt16LE(16, 34);
out.write('data', 36); out.writeUInt32LE(N * 4, 40);
for (let i = 0; i < N; i++) {
  out.writeInt16LE(Math.round(Math.max(-1, Math.min(1, mix[0][i] * norm)) * 32767), 44 + i * 4);
  out.writeInt16LE(Math.round(Math.max(-1, Math.min(1, mix[1][i] * norm)) * 32767), 46 + i * 4);
}
fs.writeFileSync(path.join(here, '../public/soundtrack.wav'), out);
console.log('wrote public/soundtrack.wav');
