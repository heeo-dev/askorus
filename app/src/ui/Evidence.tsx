// The evidence under a combined answer (S5c). Folded, it is four counts
// (F-Result-Expanded: 다른 말 · 확인 필요 · 같은 말 · 한 AI만, the differ count
// on apricot); open, the four sections with the AIs' own sentences
// (F-Result-Evidence), each saying what the card means and, when there is
// nothing in it, that there is nothing (fix ⑥).

import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { NumberSpan } from '@/api/types';
import type { CardKind, UnitView } from '@/ask/compare';

import { Button } from './Button';
import { Mark } from './controls';
import { Icon, IconButton } from './Icon';
import { themed, useTheme } from './theme';
import { UnitText } from './UnitText';

/** A folded count; count null is still being judged ('…'). */
export type TileView = { kind: CardKind; label: string; count: number | null; a11yLabel: string };

export function EvidenceCounts({ title, tiles, onOpen }: { title: string; tiles: TileView[]; onOpen: (kind: CardKind) => void }) {
  const th = useTheme();
  const s = styles(th);
  return (
    <View style={s.box}>
      <Text style={s.title} accessibilityRole="header">
        {title}
      </Text>
      <View style={s.tiles}>
        {tiles.map((x) => (
          <Pressable
            key={x.kind}
            onPress={() => onOpen(x.kind)}
            accessibilityRole="button"
            accessibilityLabel={x.a11yLabel}
            accessibilityState={{ expanded: false }}
            style={({ pressed }) => [s.tile, pressed && s.pressed]}
          >
            {x.kind === 'differ' && x.count ? (
              <Mark n={x.count} tone="fill" size="lg" />
            ) : (
              <Text style={s.tileNum} allowFontScaling={false}>
                {x.count === null ? '…' : x.count}
              </Text>
            )}
            <Text style={s.tileLabel} numberOfLines={1}>
              {x.label}
            </Text>
          </Pressable>
        ))}
      </View>
    </View>
  );
}

/** A piece of an item's second line; bold pieces are the other sides' numbers. */
export type WhoPiece = { text: string; bold?: boolean };

export type EvidenceItemView = { key: string; unit: UnitView; spans: NumberSpan[]; who: WhoPiece[]; note?: string; onPress?: () => void; a11yLabel?: string };

export type EvidenceSectionView = {
  kind: CardKind;
  label: string;
  count: number | null;
  caption: string;
  items: EvidenceItemView[];
  more?: { label: string; onPress: () => void };
  empty?: string;
  notes?: string[];
};

export function EvidenceSections({
  title,
  collapseLabel,
  onCollapse,
  info,
  sections,
}: {
  title: string;
  collapseLabel: string;
  onCollapse: () => void;
  info?: { label: string; onPress: () => void };
  sections: EvidenceSectionView[];
}) {
  const th = useTheme();
  const s = styles(th);
  return (
    <View style={s.box}>
      <View style={s.head}>
        <Text style={s.title} accessibilityRole="header">
          {title}
        </Text>
        {info ? <IconButton name="info" label={info.label} onPress={info.onPress} size={18} color={th.caption} style={s.info} /> : null}
        <Pressable
          onPress={onCollapse}
          accessibilityRole="button"
          accessibilityLabel={collapseLabel}
          accessibilityState={{ expanded: true }}
          style={({ pressed }) => [s.hide, pressed && { backgroundColor: th.raised }]}
        >
          <Text style={s.hideText}>{collapseLabel}</Text>
          <Icon name="chevronUp" size={14} color={th.textSub} />
        </Pressable>
      </View>
      <View style={s.sections}>
        {sections.map((sec) => (
          <View key={sec.kind} style={[s.section, sec.kind === 'differ' && s.sectionDiffer]} testID={`evidence-${sec.kind}`}>
            <View style={s.secHead}>
              <Mark n={sec.count === null ? '…' : sec.count} tone={sec.kind === 'differ' ? 'fill' : 'plain'} />
              <Text style={s.secName} accessibilityRole="header">
                {sec.label}
              </Text>
              <Text style={s.secCaption} numberOfLines={1}>
                {sec.caption}
              </Text>
            </View>
            {sec.items.map((it) => {
              const who = it.who.map((p) => p.text).join('');
              const body = (
                <>
                  <View style={s.itemText}>
                    <UnitText unit={it.unit} spans={it.spans} />
                    {it.who.length ? (
                      <Text style={s.who}>
                        {it.who.map((p, i) =>
                          p.bold ? (
                            <Text key={i} style={s.whoBold}>
                              {p.text}
                            </Text>
                          ) : (
                            p.text
                          ),
                        )}
                      </Text>
                    ) : null}
                    {it.note ? <Text style={s.note}>{it.note}</Text> : null}
                  </View>
                  {it.onPress ? <Icon name="chevronRight" size={16} color={th.caption} /> : null}
                </>
              );
              const label = it.a11yLabel ?? [it.unit.text, who, it.note].filter(Boolean).join('. ');
              return it.onPress ? (
                <Pressable
                  key={it.key}
                  onPress={it.onPress}
                  accessibilityRole="button"
                  accessibilityLabel={label}
                  style={({ pressed }) => [s.item, pressed && s.pressed]}
                >
                  {body}
                </Pressable>
              ) : (
                <View key={it.key} style={s.item}>
                  {body}
                </View>
              );
            })}
            {sec.items.length === 0 && sec.empty ? <Text style={s.empty}>{sec.empty}</Text> : null}
            {sec.more ? <Button variant="quiet" label={sec.more.label} onPress={sec.more.onPress} /> : null}
            {sec.notes?.map((n, i) => (
              <Text key={i} style={s.empty}>
                {n}
              </Text>
            ))}
          </View>
        ))}
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 6 },
    title: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    pressed: { opacity: 0.7 },
    tiles: { flexDirection: 'row', gap: 8 },
    tile: { flex: 1, height: 48, borderRadius: 12, backgroundColor: th.surface, alignItems: 'center', justifyContent: 'center', gap: 1 },
    tileNum: { fontSize: 17, fontWeight: '600', lineHeight: 22, color: th.text, fontVariant: ['tabular-nums'] },
    tileLabel: { fontSize: 12, lineHeight: 17, color: th.textSub },
    head: { flexDirection: 'row', alignItems: 'center', marginRight: -12 },
    info: { marginLeft: -6 },
    hide: { height: 44, marginLeft: 'auto', flexDirection: 'row', alignItems: 'center', gap: 4, paddingHorizontal: 12, borderRadius: 22 },
    hideText: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.textSub },
    sections: { gap: 8 },
    section: { paddingVertical: 2, paddingHorizontal: 4, borderRadius: 14, backgroundColor: th.surface },
    sectionDiffer: { borderWidth: 1, borderColor: th.softLine },
    secHead: { minHeight: 38, flexDirection: 'row', alignItems: 'center', gap: 8, paddingHorizontal: 10 },
    secName: { flexShrink: 0, fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    secCaption: { flexShrink: 1, minWidth: 0, fontSize: 12, lineHeight: 17, color: th.caption },
    item: {
      minHeight: 44,
      flexDirection: 'row',
      alignItems: 'center',
      gap: 8,
      paddingTop: 6,
      paddingBottom: 6,
      paddingLeft: 10,
      paddingRight: 8,
      borderTopWidth: 1,
      borderTopColor: th.divider,
    },
    itemText: { flex: 1, minWidth: 0, gap: 2 },
    who: { fontSize: 13, lineHeight: 18.2, color: th.caption },
    whoBold: { fontWeight: '600', color: th.text },
    note: { fontSize: 13, lineHeight: 18.2, color: th.caption, fontStyle: 'italic' },
    empty: { paddingHorizontal: 10, paddingTop: 2, paddingBottom: 10, fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
