import { Composition } from 'remotion';
import { Promo } from './Promo';
import { T } from './time';

export const Root = () => (
  <Composition id="Promo" component={Promo} durationInFrames={Math.round(T.duration * T.fps)} fps={T.fps} width={1920} height={1080} />
);
