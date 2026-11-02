// The brand (S5c): the wordmark 'Asksemble' and the app icon B as a mark.
// Both are PNGs drawn once on the PC by scripts/render-brand.mjs from the
// design boards (F-Brand-Wordmark, F-Brand-Icon B), at 1x/2x/3x: the
// wordmark is Noto Sans KR 800, so the app ships no font file and loads
// none (Expo Go). wordmark-size.json is the 1x box the script measured.

import { Image, type ImageSourcePropType } from 'react-native';

import { t } from '@/i18n';

import { type Scheme, useTheme } from './theme';
import box from './wordmark-size.json';

const wordmarks: Record<Scheme, ImageSourcePropType> = {
  // 'Ask' #FFAD85 + 'semble' #EDEBE7 on dark; 'Ask' #DD5A26 + 'semble' #1D1A17 on light.
  dark: require('../../assets/brand/wordmark-dark.png'),
  light: require('../../assets/brand/wordmark-light.png'),
};

const mark: ImageSourcePropType = require('../../assets/brand/mark.png');

/** Wordmark draws 'Asksemble' at the given font size (px) in the theme's colours. */
export function Wordmark({ size = box.fontSize, scheme, testID }: { size?: number; scheme?: Scheme; testID?: string }) {
  const th = useTheme();
  const s = scheme ?? th.scheme;
  const k = size / box.fontSize;
  return (
    <Image
      source={wordmarks[s]}
      style={{ width: box.width * k, height: box.height * k }}
      resizeMode="contain"
      accessible
      accessibilityRole="image"
      accessibilityLabel={t('app.name')}
      testID={testID ?? `wordmark-${s}`}
    />
  );
}

/** BrandMark is the app icon (apricot squircle, ink bubble); decoration only. */
export function BrandMark({ size = 112, testID = 'brand-mark', onLoad }: { size?: number; testID?: string; onLoad?: () => void }) {
  return (
    <Image
      source={mark}
      onLoad={onLoad}
      style={{ width: size, height: size }}
      resizeMode="contain"
      accessible={false}
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      testID={testID}
    />
  );
}
