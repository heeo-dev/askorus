// One unit an AI wrote (an AI's own sentence), as the evidence and the
// differ rows quote it (S5c; the drawing of CompareItem.UnitText, restyled):
// selectable plain text, math only where the PC marked it (S1 decision 1),
// the numbers that differ in bold, and a table row's or nested item's
// context small above it.

import { Platform, type StyleProp, StyleSheet, Text, type TextStyle, View } from 'react-native';

import type { NumberSpan } from '@/api/types';
import { pieces, type UnitView } from '@/ask/compare';

import { texToUnicode } from './tex';
import { themed, useTheme } from './theme';

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

const styles = themed((th) =>
  StyleSheet.create({
    unit: { gap: 2 },
    context: { color: th.caption, fontSize: 12, lineHeight: 17 },
    text: { color: th.text, fontSize: 14, lineHeight: 20.3 },
    bold: { fontWeight: '700' },
    code: { fontFamily: mono, fontSize: 13, backgroundColor: th.raised, color: th.text },
  }),
);
