import { loadFont as loadInter } from '@remotion/google-fonts/Inter';
import { loadFont as loadMono } from '@remotion/google-fonts/JetBrainsMono';

const latin = { subsets: ['latin' as const] };

export const font = {
  sans: loadInter('normal', { weights: ['500', '700', '800'], ...latin }).fontFamily,
  mono: loadMono('normal', { weights: ['400', '700', '800'], ...latin }).fontFamily,
};

/** visor's own dark tokens, plus the takes' terminal background. */
export const color = {
  term: '#23222e',
  bg: '#13171d',
  sel: '#222a36',
  line: '#3d4757',
  faint: '#4e5766',
  dim: '#7e8797',
  fg: '#d3d9e2',
  bright: '#f5f7fa',
  accent: '#8aa9ff',
  warn: '#e4ae5c',
  ok: '#86c497',
  bad: '#ef7d7d',
  info: '#b59dff',
};
