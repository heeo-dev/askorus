// One item of a Summary card (P2-3) and the unit text it quotes. A unit is
// an AI's own sentence, drawn as selectable plain text: math only where the
// PC marked it (S1 decision 1), the numbers that differ in bold, and a
// table row's or nested item's context small above it.

import { Platform, Pressable, type StyleProp, StyleSheet, Text, type TextStyle, View } from 'react-native';

import type { NumberSpan } from '@/api/types';
import { type ItemView, pieces, type UnitView, whyText } from '@/ask/compare';
import { t } from '@/i18n';

import { texToUnicode } from './tex';
import { type Theme, useTheme } from './theme';

const mono = Platform.select({ ios: 'Menlo', default: 'monospace' });

/** UnitText draws one unit; spans are the number spans of its side (only this unit's are used). */
export function UnitText({ unit, spans, style }: { unit: UnitView; spans: NumberSpan[]; style?: StyleProp<TextStyle> }) {
  const s = styles(useTheme());
  const parts = pieces(
    unit.text,
    unit.math,
    spans.filter((x) => x.unit === unit.id),
  );
  return (
    <View style={s.unit}>
      {unit.context ? (
        <Text style={s.context} numberOfLines={2}>
          {unit.context}
        </Text>
      ) : null}
      <Text style={[s.text, style]} selectable>
        {parts.map((p, i) => {
          if (p.math) {
            // As in an answer: Unicode text, or the TeX as code when it cannot be shown.
            const text = texToUnicode(p.math.tex);
            return text === null ? (
              <Text key={i} style={[s.code, p.bold && s.bold]}>
                {p.math.tex}
              </Text>
            ) : (
              <Text key={i} style={p.bold ? s.bold : undefined}>
                {text}
              </Text>
            );
          }
          return p.bold ? (
            <Text key={i} style={s.bold}>
              {p.text}
            </Text>
          ) : (
            p.text
          );
        })}
      </Text>
    </View>
  );
}

const unique = (xs: string[]) => xs.filter((x, i) => xs.indexOf(x) === i);

export function CompareItem({ item, onPair }: { item: ItemView; onPair: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const side = item.sides[0];
  const unit = side.units[0];
  const quote = <UnitText unit={unit} spans={side.numberSpans} />;
  const missing = item.missing.length ? (
    <Text style={s.note}>{t('compare.cantCompare', { names: item.missing.join(', ') })}</Text>
  ) : null;

  if (item.kind === 'differ') {
    return (
      <View style={s.item}>
        {quote}
        <Text style={s.who}>{item.sides.map((x) => x.agents.join(', ')).join(' ↔ ')}</Text>
        {item.numbersDiffer ? <Text style={s.warn}>{t('compare.numbersDiffer')}</Text> : null}
        <Pressable onPress={onPair} accessibilityRole="button" hitSlop={6} style={({ pressed }) => [s.linkBox, pressed && s.pressed]}>
          <Text style={s.link}>{t('compare.sideBySide')} ›</Text>
        </Pressable>
        {missing}
      </View>
    );
  }
  if (item.kind === 'check') {
    const why = item.check
      .map(whyText)
      .filter((x): x is string => !!x)
      .join(' · ');
    return (
      <Pressable onPress={onPair} accessibilityRole="button" style={({ pressed }) => [s.item, pressed && s.pressed]}>
        {quote}
        <Text style={s.who}>{t('compare.sentenceOf', { name: unit.agent })}</Text>
        {why ? <Text style={s.why}>{why}</Text> : null}
        {missing}
      </Pressable>
    );
  }
  const agents = unique(item.sides.flatMap((x) => x.agents));
  return (
    <View style={s.item}>
      {quote}
      <Text style={s.who}>{item.kind === 'onlyOne' ? t('compare.only', { name: agents[0] ?? unit.agent }) : agents.join(', ')}</Text>
      {missing}
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    item: { gap: 4, paddingTop: 10, borderTopColor: th.border, borderTopWidth: StyleSheet.hairlineWidth },
    unit: { gap: 2 },
    context: { color: th.muted, fontSize: 12, lineHeight: 17 },
    text: { color: th.text, fontSize: 15, lineHeight: 22 },
    bold: { fontWeight: '700' },
    code: { fontFamily: mono, fontSize: 14, backgroundColor: th.bg, color: th.text },
    who: { color: th.muted, fontSize: 13, lineHeight: 18 },
    warn: { color: th.danger, fontSize: 13, lineHeight: 18, fontWeight: '600' },
    why: { color: th.text, fontSize: 13, lineHeight: 18 },
    note: { color: th.muted, fontSize: 13, lineHeight: 18, fontStyle: 'italic' },
    linkBox: { alignSelf: 'flex-start', paddingVertical: 2 },
    link: { color: th.accent, fontSize: 14, fontWeight: '600' },
    pressed: { opacity: 0.6 },
  });
