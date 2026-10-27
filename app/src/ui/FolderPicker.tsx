// Picks a folder for a new session: the PC's allowed folders first, then
// folders that already have conversations, most recent first.

import { Pressable, StyleSheet, Text, View } from 'react-native';

import type { Folder } from '@/api/types';
import { t } from '@/i18n';

import { ago } from './format';
import { type Theme, useTheme } from './theme';

export const isHistory = (f: Folder) => f.source === 'history';

export function FolderPicker({
  folders,
  value,
  onChange,
}: {
  folders: Folder[];
  value: string;
  onChange: (id: string) => void;
}) {
  const s = styles(useTheme());
  const allowed = folders.filter((f) => !isHistory(f));
  const recent = folders.filter(isHistory);

  const row = (f: Folder) => {
    const on = f.id === value;
    return (
      <Pressable
        key={f.id}
        onPress={() => onChange(f.id)}
        style={[s.row, on && s.rowOn]}
        accessibilityRole="radio"
        accessibilityState={{ checked: on }}
        accessibilityLabel={f.name || f.id}
      >
        <Text style={[s.name, on && s.nameOn]}>{f.name || f.id}</Text>
        <Text style={s.path} numberOfLines={1}>
          {f.lastActive ? `${ago(f.lastActive)} · ${f.path}` : f.path}
        </Text>
      </Pressable>
    );
  };

  return (
    <View style={s.box} accessibilityRole="radiogroup">
      {allowed.length ? <Text style={s.group}>{t('new.allowedFolders')}</Text> : null}
      {allowed.map(row)}
      {recent.length ? <Text style={s.group}>{t('new.recentFolders')}</Text> : null}
      {recent.map(row)}
    </View>
  );
}

const styles = (th: Theme) =>
  StyleSheet.create({
    box: { gap: 6 },
    group: { color: th.muted, fontSize: 13, marginTop: 6 },
    row: {
      backgroundColor: th.card,
      borderColor: th.border,
      borderWidth: 1,
      borderRadius: 10,
      paddingHorizontal: 12,
      paddingVertical: 9,
      gap: 2,
    },
    rowOn: { borderColor: th.accent, borderWidth: 2 },
    name: { color: th.text, fontSize: 15, fontWeight: '600' },
    nameOn: { color: th.accent },
    path: { color: th.muted, fontSize: 12 },
  });
