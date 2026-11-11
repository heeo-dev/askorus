// A PC's name on this phone (#53): the PC's own host name until the person
// types another, so two PCs that call themselves the same can be told
// apart. The name stays on this phone; the PC is not told. Reached from
// Settings' PC sheet. The save button sits in a bar that rises with the
// keyboard, like the new-session form's.

import { router, useLocalSearchParams } from 'expo-router';
import { useEffect, useState } from 'react';
import { StyleSheet, Text, TextInput, View } from 'react-native';

import { cleanPCName, maxPCName, osName } from '@/connections/label';
import { type Connection, ConnectionStore } from '@/connections/store';
import { t } from '@/i18n';
import { Button } from '@/ui/Button';
import { FormScroll } from '@/ui/FormScroll';
import { TopBar } from '@/ui/Header';
import { useKeyboardInset, useSafeInsets } from '@/ui/keyboard';
import { SectionLabel } from '@/ui/List';
import { renamePC } from '@/ui/shell/pcs';
import { themed, useTheme } from '@/ui/theme';

const store = new ConnectionStore();

export default function RenamePC() {
  const th = useTheme();
  const s = styles(th);
  const insets = useSafeInsets();
  const keyboard = useKeyboardInset();
  const { id } = useLocalSearchParams<{ id: string }>();
  const [conn, setConn] = useState<Connection | null>(null);
  const [name, setName] = useState('');

  useEffect(() => {
    let live = true;
    store.get(id).then((c) => {
      if (!live || !c) return;
      setConn(c);
      setName(c.name);
    });
    return () => {
      live = false;
    };
  }, [id]);

  const clean = cleanPCName(name);
  const save = async () => {
    if (!conn || !clean) return;
    if (clean !== conn.name) await renamePC(conn.id, clean);
    router.back();
  };

  return (
    <View style={s.screen}>
      <TopBar title={t('settings.rename')} onBack={() => router.back()} />
      <FormScroll style={s.scroll} contentContainerStyle={s.body}>
        <View style={s.field}>
          <SectionLabel text={t('settings.pcName')} />
          <TextInput
            style={s.input}
            value={name}
            onChangeText={setName}
            accessibilityLabel={t('settings.pcName')}
            maxLength={maxPCName}
            returnKeyType="done"
            onSubmitEditing={save}
            autoCorrect={false}
            testID="pc-name"
          />
          {conn ? (
            <Text style={s.caption}>
              {[osName(conn.os), `${conn.host}:${conn.port}`].filter(Boolean).join(' · ')}
            </Text>
          ) : null}
          <Text style={s.caption}>{t('settings.renameHint')}</Text>
        </View>
      </FormScroll>
      <View style={[s.bar, { paddingBottom: 16 + Math.max(keyboard, insets.bottom) }]}>
        <Button label={t('settings.saveName')} onPress={save} disabled={!conn || !clean} />
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    screen: { flex: 1, backgroundColor: th.bg },
    scroll: { flex: 1 },
    body: { gap: 22, paddingTop: 10, paddingHorizontal: 16, paddingBottom: 16 },
    field: { gap: 8 },
    caption: { fontSize: 13, lineHeight: 18.2, color: th.caption },
    input: {
      height: 52,
      paddingHorizontal: 16,
      paddingVertical: 0,
      borderRadius: 16,
      borderWidth: 1,
      borderColor: th.border,
      backgroundColor: th.surface,
      fontSize: 15,
      color: th.text,
    },
    bar: { gap: 8, paddingTop: 12, paddingHorizontal: 16, borderTopWidth: 1, borderTopColor: th.barLine, backgroundColor: th.bg },
  }),
);
