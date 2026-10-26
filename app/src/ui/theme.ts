import { useColorScheme } from 'react-native';

export type Theme = {
  bg: string;
  card: string;
  text: string;
  muted: string;
  border: string;
  accent: string;
  accentText: string;
  danger: string;
  ok: string;
};

const light: Theme = {
  bg: '#f6f6f4',
  card: '#ffffff',
  text: '#1d1d1b',
  muted: '#6b6b66',
  border: '#e2e1dc',
  accent: '#2f5bd3',
  accentText: '#ffffff',
  danger: '#b3261e',
  ok: '#2e7d32',
};

const dark: Theme = {
  bg: '#141413',
  card: '#1f1f1d',
  text: '#ecebe6',
  muted: '#a3a29b',
  border: '#33332f',
  accent: '#7d9cf0',
  accentText: '#0f1630',
  danger: '#f2b8b5',
  ok: '#8bd38f',
};

export function useTheme(): Theme {
  return useColorScheme() === 'dark' ? dark : light;
}
