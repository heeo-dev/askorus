// The AIs answering a question that will be compared (S5c,
// F-Ask-Answering): '답하는 중…', one 52 px row per AI (its avatar inside a
// ring that turns while it answers, its name and model, and the seconds at
// the right, with a check once it answered), and under them what happens
// once every answer is in. A screen reader hears each row's state as its
// value, which is always an object (R-40).

import { StyleSheet, Text, View } from 'react-native';

import { type PartView, rowValue } from '@/ask/present';
import { t } from '@/i18n';

import { AvatarRing } from './Avatar';
import { Icon } from './Icon';
import { themed, useTheme } from './theme';

export function AnsweringList({ rows, heading, caption }: { rows: PartView[]; heading?: string; caption?: string }) {
  const th = useTheme();
  const s = styles(th);
  return (
    <View style={s.box}>
      {heading ? (
        <Text style={s.heading} accessibilityRole="header">
          {heading}
        </Text>
      ) : null}
      <View style={s.list}>
        {rows.map((r, i) => (
          <View
            key={r.agent}
            style={[s.row, i > 0 && s.divided]}
            accessible
            accessibilityLabel={r.model ? `${r.name}, ${r.model}` : r.name}
            accessibilityValue={rowValue(r)}
            accessibilityState={{ busy: r.state === 'answering' }}
            testID={`answering-${r.agent}`}
          >
            <AvatarRing agent={r.agent} busy={r.state === 'answering'} />
            <View style={s.names}>
              <Text style={s.name} numberOfLines={1}>
                {r.name}
              </Text>
              {r.model ? (
                <Text style={s.model} numberOfLines={1}>
                  {r.model}
                </Text>
              ) : null}
            </View>
            {r.state === 'failed' ? (
              <Text style={s.failed} numberOfLines={1}>
                {t('compare.tab.failed')}
              </Text>
            ) : (
              <View style={s.time}>
                {r.state === 'done' ? <Icon name="check" size={16} color={th.ok} /> : null}
                <Text style={s.timeText}>{t('ask.seconds', { s: r.s })}</Text>
              </View>
            )}
          </View>
        ))}
      </View>
      {caption ? <Text style={s.caption}>{caption}</Text> : null}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 8 },
    heading: { minHeight: 24, fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text, textAlignVertical: 'center' },
    list: { paddingVertical: 2, paddingHorizontal: 4, borderRadius: 14, backgroundColor: th.surface },
    row: { minHeight: 52, flexDirection: 'row', alignItems: 'center', gap: 12, paddingLeft: 8, paddingRight: 14 },
    divided: { borderTopWidth: 1, borderTopColor: th.divider },
    names: { flex: 1, minWidth: 0, flexDirection: 'row', alignItems: 'baseline', gap: 8 },
    name: { flexShrink: 0, fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    model: { flexShrink: 1, fontSize: 12, lineHeight: 17, color: th.caption },
    time: { flexDirection: 'row', alignItems: 'center', gap: 6 },
    timeText: { fontSize: 14, lineHeight: 20, color: th.textSub, fontVariant: ['tabular-nums'] },
    failed: { flexShrink: 1, fontSize: 14, lineHeight: 20, color: th.danger },
    caption: { paddingHorizontal: 4, fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
