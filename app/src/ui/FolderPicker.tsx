// Picks a folder for a new session (S5c, F-PC-New): the PC's allowed
// folders in one block, then the folders that already have conversations
// (most recent first, with when) under their own label. Each is a radio
// row: a 22 px ring that fills when picked, the name and the path.

import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { Folder } from '@/api/types';
import { t } from '@/i18n';

import { ago } from './format';
import { Group, SectionLabel } from './List';
import { themed, useTheme } from './theme';

export const isHistory = (f: Folder) => f.source === 'history';

export function FolderPicker({ folders, value, onChange }: { folders: Folder[]; value: string; onChange: (id: string) => void }) {
  const s = styles(useTheme());
  const allowed = folders.filter((f) => !isHistory(f));
  const recent = folders.filter(isHistory);

  const row = (f: Folder) => {
    const on = f.id === value;
    const name = f.name || f.id;
    return (
      <Pressable
        key={f.id}
        onPress={() => onChange(f.id)}
        style={({ pressed }) => [s.row, pressed && s.pressed]}
        accessibilityRole="radio"
        accessibilityState={{ checked: on }}
        accessibilityLabel={name}
        accessibilityHint={f.path}
      >
        <View style={[s.radio, on && s.radioOn]}>{on ? <View style={s.radioDot} /> : null}</View>
        <View style={s.names}>
          <Text style={s.name} numberOfLines={1}>
            {name}
          </Text>
          <Text style={s.path} numberOfLines={1}>
            {f.lastActive ? `${ago(f.lastActive)} · ${f.path}` : f.path}
          </Text>
        </View>
      </Pressable>
    );
  };

  return (
    <View style={s.box} accessibilityRole="radiogroup">
      {allowed.length ? <Group>{allowed.map(row)}</Group> : null}
      {recent.length ? (
        <>
          <View style={allowed.length ? s.recentLabel : undefined}>
            <SectionLabel text={t('new.recentFolders')} />
          </View>
          <Group>{recent.map(row)}</Group>
        </>
      ) : null}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 8 },
    recentLabel: { marginTop: 6 },
    row: { minHeight: 60, flexDirection: 'row', alignItems: 'center', gap: 14, paddingHorizontal: 16 },
    pressed: { backgroundColor: th.raised },
    // Picked: the ring and the dot in the mark colour (apricot; deep apricot
    // on light, where apricot does not stand out), the surface between them.
    radio: { width: 22, height: 22, borderRadius: 11, borderWidth: 2, borderColor: th.caption, alignItems: 'center', justifyContent: 'center' },
    radioOn: { borderColor: th.mark },
    radioDot: { width: 10, height: 10, borderRadius: 5, backgroundColor: th.mark },
    names: { flex: 1, minWidth: 0, gap: 2 },
    name: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    path: { fontSize: 13, lineHeight: 18, color: th.caption },
  }),
);
