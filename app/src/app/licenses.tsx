// Open-source licenses (from Settings › About, #32): everything the app is
// built from, with each license. The licenses ask that their notice goes
// with every copy, and the installed app is a copy. The list is
// src/licenses/licenses.json, made by scripts/licenses.mjs from the
// packages themselves: the packages of package-lock.json that are not
// dev-only, the Android libraries of the release build, and the fonts and
// icon shapes. Tapping an item shows its license text in a sheet.

import { router } from 'expo-router';
import { useMemo, useState } from 'react';
import { FlatList, Pressable, StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';
import { reflow } from '@/licenses/reflow';
import { TopBar } from '@/ui/Header';
import { Icon } from '@/ui/Icon';
import { useSafeInsets } from '@/ui/keyboard';
import { Sheet } from '@/ui/Sheet';
import { font, themed, useTheme } from '@/ui/theme';

export type LicenseKind = 'npm' | 'android' | 'go' | 'other';
export type LicenseItem = { n: string; v: string; l: string; t: number | null; k: LicenseKind; u?: string };
export type Licenses = { packages: LicenseItem[]; texts: string[] };

// Read with require: the file is large, and nothing of its shape is needed
// beyond the type above.
const data = require('@/licenses/licenses.json') as Licenses;

const kinds: LicenseKind[] = ['npm', 'android', 'go', 'other'];

type Line = { key: string; head: LicenseKind; count: number } | { key: string; item: LicenseItem };

export default function LicensesScreen() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const [open, setOpen] = useState<LicenseItem | null>(null);
  const back = () => (router.canGoBack() ? router.back() : router.replace('/'));

  const lines = useMemo<Line[]>(() => {
    const out: Line[] = [];
    for (const k of kinds) {
      const items = data.packages.filter((p) => p.k === k);
      if (!items.length) continue;
      out.push({ key: `head-${k}`, head: k, count: items.length });
      items.forEach((item, i) => out.push({ key: `${k}-${i}`, item }));
    }
    return out;
  }, []);

  // Shown with its paragraphs joined again (reflow): the stored text is
  // wrapped for a wide screen.
  const text = useMemo(() => (open && open.t !== null ? reflow(data.texts[open.t]) : ''), [open]);

  return (
    <View style={s.screen}>
      <TopBar title={t('licenses.title')} onBack={back} />
      <FlatList
        data={lines}
        keyExtractor={(l) => l.key}
        initialNumToRender={24}
        windowSize={9}
        contentContainerStyle={{ paddingBottom: 24 + insets.bottom }}
        ListHeaderComponent={<Text style={s.intro}>{t('licenses.intro')}</Text>}
        renderItem={({ item: line }) =>
          'head' in line ? (
            <Text style={s.head} accessibilityRole="header">
              {t(`licenses.kind.${line.head}`, { n: line.count })}
            </Text>
          ) : (
            <Pressable
              onPress={() => setOpen(line.item)}
              accessibilityRole="button"
              accessibilityLabel={`${line.item.n}, ${line.item.l || t('licenses.unknown')}`}
              style={({ pressed }) => [s.row, pressed && s.pressed]}
            >
              <View style={s.names}>
                <Text style={s.name} numberOfLines={1} ellipsizeMode="middle">
                  {line.item.n}
                </Text>
                <Text style={s.sub} numberOfLines={1}>
                  {[line.item.v, line.item.l || t('licenses.unknown')].filter(Boolean).join(' · ')}
                </Text>
              </View>
              <Icon name="chevronRight" size={18} color={th.caption} />
            </Pressable>
          )
        }
      />
      <Sheet
        open={!!open}
        onClose={() => setOpen(null)}
        title={open?.n}
        subtitle={open ? [open.v, open.l || t('licenses.unknown')].filter(Boolean).join(' · ') : undefined}
        testID="license-sheet"
      >
        <View style={s.sheetBody}>
          {text ? (
            <Text style={s.text} selectable>
              {text}
            </Text>
          ) : (
            <>
              <Text style={s.noText}>{t('licenses.noText')}</Text>
              {open?.u ? (
                <Text style={s.text} selectable>
                  {open.u}
                </Text>
              ) : null}
            </>
          )}
        </View>
      </Sheet>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    intro: { paddingHorizontal: 20, paddingTop: 8, paddingBottom: 4, fontSize: 15, lineHeight: 22.5, color: th.textSub },
    head: { paddingHorizontal: 20, paddingTop: 22, paddingBottom: 6, fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    row: { minHeight: 56, flexDirection: 'row', alignItems: 'center', gap: 12, paddingLeft: 20, paddingRight: 14, paddingVertical: 8 },
    pressed: { backgroundColor: th.raised },
    names: { flex: 1, minWidth: 0, gap: 2 },
    name: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    sub: { ...font.caption, color: th.caption },
    sheetBody: { paddingHorizontal: 20, paddingBottom: 8, gap: 10 },
    text: { fontSize: 12.5, lineHeight: 18, color: th.textSub },
    noText: { fontSize: 15, lineHeight: 22.5, color: th.textSub },
  }),
);
