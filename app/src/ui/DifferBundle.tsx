// Where the AIs' answers differ, under the combined answer (C5, #11: the
// user's pick on the C5 boards, '2 링크 줄, 주제 보임'). One link line, the
// count in an apricot circle and 'AI 답이 다른 곳 ›', with the places'
// topics in a grey line under it. Pressed, it opens a panel with an item
// per place: its topic and a few words per side (avatars and the gist); an
// item opens the AIs' own sentences, each leading to that AI's answer.
// Without topics (still being named, failed, an older agent) the line is
// the count alone and the items are titled by their headline.
//
// Every control is a Pressable with its own label and expanded state, so
// TalkBack reaches and opens each (R-47 was a highlight inside a Text).

import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { NumberSpan } from '@/api/types';
import type { UnitView } from '@/ask/compare';
import type { DifferBundleView } from '@/ask/present';
import { t } from '@/i18n';

import { Avatar, AvatarPile } from './Avatar';
import { Icon } from './Icon';
import { themed, useTheme } from './theme';
import { UnitText } from './UnitText';

export type DifferRowView = { key: string; agent: string; name: string; unit: UnitView; spans: NumberSpan[]; onPress: () => void };

export function DifferBundle({
  view,
  open,
  onToggle,
  opened,
  onToggleItem,
  onOpenAnswer,
}: {
  view: DifferBundleView;
  open: boolean;
  onToggle: () => void;
  /** The keys of the items whose sentences are open. */
  opened: string[];
  onToggleItem: (key: string) => void;
  /** Opens an AI's answer (by its code). */
  onOpenAnswer: (agent: string) => void;
}) {
  const th = useTheme();
  const s = styles(th);
  const topics = view.topics.join(' · ');
  const label = topics ? t('compare.differLinkTopics', { n: view.count, topics }) : t('compare.differLinkCount', { n: view.count });
  // Rows are split by the panel's line on light, by the surface divider on dark (boards).
  const rowLine = th.scheme === 'dark' ? th.divider : th.differLine;
  return (
    <View style={s.box}>
      <Pressable onPress={onToggle} accessibilityRole="button" accessibilityLabel={label} accessibilityState={{ expanded: open }} style={({ pressed }) => [s.link, pressed && s.pressed]} testID="differ-link">
        <View style={s.line}>
          <View style={s.count}>
            <Text style={s.countText} allowFontScaling={false}>
              {view.count}
            </Text>
          </View>
          <Text style={s.title}>{t('compare.differLink')}</Text>
          <Icon name={open ? 'chevronUp' : 'chevronRight'} size={16} color={th.accent} />
        </View>
        {topics && !open ? (
          <Text style={s.topics} numberOfLines={3}>
            {topics}
          </Text>
        ) : null}
      </Pressable>
      {open ? (
        <View style={s.panel} accessibilityLabel={t('compare.differGroup')} testID="differ-panel">
          {view.items.map((it, i) => {
            const on = opened.includes(it.key);
            const gists = it.gists.map((g) => `${g.agents.map(nameOf(it)).join(', ')} ${g.text}`).join(', ');
            return (
              <View key={it.key} style={i > 0 && { borderTopWidth: 1, borderTopColor: th.differLine }}>
                <Pressable
                  onPress={() => onToggleItem(it.key)}
                  accessibilityRole="button"
                  accessibilityLabel={gists ? `${it.title}: ${gists}` : it.title}
                  accessibilityState={{ expanded: on }}
                  style={({ pressed }) => [s.item, pressed && s.pressed]}
                  testID={`differ-item-${i}`}
                >
                  <View style={s.itemText}>
                    <Text style={s.topic}>{it.title}</Text>
                    {it.gists.length ? (
                      <View style={s.gists}>
                        {it.gists.map((g, j) => (
                          <View key={j} style={s.gist}>
                            <AvatarPile agents={g.agents} ring={th.differPanel} />
                            <Text style={s.gistText}>{g.text}</Text>
                          </View>
                        ))}
                      </View>
                    ) : null}
                  </View>
                  <Icon name={on ? 'chevronUp' : 'chevronDown'} size={16} color={th.caption} />
                </Pressable>
                {on ? (
                  <View style={s.rows}>
                    {it.rows.map((r, k) => (
                      <Pressable
                        key={r.key}
                        onPress={() => onOpenAnswer(r.agent)}
                        accessibilityRole="button"
                        accessibilityLabel={`${r.name}: ${r.unit.text}`}
                        style={({ pressed }) => [s.row, k > 0 && { borderTopWidth: 1, borderTopColor: rowLine }, pressed && s.pressed]}
                      >
                        <Avatar agent={r.agent} tone="onDiffer" />
                        <View style={s.rowText}>
                          <Text style={s.name}>{r.name}</Text>
                          <UnitText unit={r.unit} spans={r.spans} />
                        </View>
                        <Icon name="chevronRight" size={16} color={th.caption} />
                      </Pressable>
                    ))}
                  </View>
                ) : null}
              </View>
            );
          })}
        </View>
      ) : null}
    </View>
  );
}

// nameOf is an AI's display name in an item, by its code, from the item's
// rows; the code itself when no row has it.
const nameOf = (it: DifferBundleView['items'][number]) => (code: string) => it.rows.find((r) => r.agent === code)?.name ?? code;

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 8 },
    link: { gap: 2, paddingVertical: 4, alignSelf: 'stretch' },
    line: { flexDirection: 'row', alignItems: 'center', gap: 6, minHeight: 32 },
    count: { minWidth: 22, height: 22, borderRadius: 11, paddingHorizontal: 6, backgroundColor: th.fill, alignItems: 'center', justifyContent: 'center' },
    countText: { fontSize: 12, lineHeight: 14, fontWeight: '600', color: th.onFill, includeFontPadding: false },
    title: { fontSize: 15, lineHeight: 21, fontWeight: '600', color: th.accent },
    topics: { paddingLeft: 28, fontSize: 13.5, lineHeight: 20, color: th.textSub },
    panel: { paddingVertical: 2, paddingHorizontal: 4, borderRadius: 14, backgroundColor: th.differPanel, borderWidth: 1, borderColor: th.differLine },
    item: { flexDirection: 'row', alignItems: 'center', gap: 10, minHeight: 48, paddingVertical: 9, paddingHorizontal: 10 },
    itemText: { flex: 1, minWidth: 0, gap: 4 },
    topic: { fontSize: 14.5, lineHeight: 20, fontWeight: '700', color: th.text },
    gists: { flexDirection: 'row', flexWrap: 'wrap', columnGap: 12, rowGap: 4 },
    gist: { flexDirection: 'row', alignItems: 'center', gap: 6, maxWidth: '100%' },
    gistText: { flexShrink: 1, fontSize: 13.5, lineHeight: 20, color: th.textSub },
    rows: { marginHorizontal: 6, marginBottom: 8, borderRadius: 12, backgroundColor: th.bg },
    row: { minHeight: 58, flexDirection: 'row', alignItems: 'center', gap: 10, paddingVertical: 7, paddingLeft: 10, paddingRight: 8 },
    rowText: { flex: 1, minWidth: 0, gap: 1 },
    name: { fontSize: 13, lineHeight: 18.2, color: th.textSub },
    pressed: { opacity: 0.7 },
  }),
);
