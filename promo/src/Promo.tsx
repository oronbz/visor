import React from 'react';
import { AbsoluteFill, Audio, Sequence, staticFile } from 'remotion';
import { Approve, Ask, Configure, End, Hook, Jira, Mouse, Open, Plugin, Shai, Title } from './scenes';
import { Vignette } from './ui';
import { S, T } from './time';

const SCENES: [keyof typeof S, React.FC][] = [
  ['hook', Hook],
  ['title', Title],
  ['open', Open],
  ['approve', Approve],
  ['jira', Jira],
  ['mouse', Mouse],
  ['ask', Ask],
  ['configure', Configure],
  ['plugin', Plugin],
  ['shai', Shai],
  ['end', End],
];

export const Promo = () => (
  <AbsoluteFill style={{ background: '#23222e' }}>
    {SCENES.map(([name, Scene]) => {
      const from = Math.round(S[name][0] * T.fps);
      const to = Math.round(S[name][1] * T.fps);
      return (
        <Sequence key={name} from={from} durationInFrames={to - from} name={name}>
          <Scene />
        </Sequence>
      );
    })}
    <Vignette />
    <Audio src={staticFile('soundtrack.wav')} />
  </AbsoluteFill>
);
