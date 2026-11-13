// What Tailscale is, for someone who has never seen it (#57): what it is,
// why Askorus needs it, what protects the connection, what Tailscale itself
// learns, and where the password goes. The sign-in card links to it right
// where the person is asked to sign in to a service they may not know;
// Settings > Tailscale shows the same text.

import { useState } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { type MessageKey, t } from '@/i18n';

import { Icon } from './Icon';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

const blocks: Array<[MessageKey, MessageKey]> = [
  ['tailnet.about.what', 'tailnet.about.whatBody'],
  ['tailnet.about.why', 'tailnet.about.whyBody'],
  ['tailnet.about.safe', 'tailnet.about.safeBody'],
  ['tailnet.about.knows', 'tailnet.about.knowsBody'],
  ['tailnet.about.password', 'tailnet.about.passwordBody'],
];

/** The explanation itself: five short questions with their answers. */
export function TailnetAboutText() {
  const s = styles(useTheme());
  return (
    <View style={s.blocks}>
      {blocks.map(([head, body]) => (
        <View key={head} style={s.block}>
          <Text style={s.head} accessibilityRole="header">
            {t(head)}
          </Text>
          <Text style={s.body}>{t(body)}</Text>
        </View>
      ))}
    </View>
  );
}

/** A line of words that opens the explanation in a sheet. */
export function TailnetAboutLink() {
  const th = useTheme();
  const s = styles(th);
  const [open, setOpen] = useState(false);
  return (
    <>
      <Pressable
        onPress={() => setOpen(true)}
        accessibilityRole="button"
        accessibilityLabel={t('tailnet.aboutLink')}
        hitSlop={10}
        style={({ pressed }) => [s.link, pressed && s.pressed]}
      >
        <Icon name="info" size={16} color={th.accent} />
        <Text style={s.linkText}>{t('tailnet.aboutLink')}</Text>
      </Pressable>
      <Sheet open={open} onClose={() => setOpen(false)} title={t('tailnet.aboutLink')} testID="tailnet-about">
        <View style={s.sheetBody}>
          <TailnetAboutText />
        </View>
      </Sheet>
    </>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    blocks: { gap: 16 },
    block: { gap: 4 },
    head: { fontSize: 15, fontWeight: '600', lineHeight: 21, color: th.text },
    body: { fontSize: 14, lineHeight: 20.3, color: th.textSub },
    link: { flexDirection: 'row', alignItems: 'center', gap: 6, alignSelf: 'flex-start', minHeight: 28 },
    linkText: { fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.accent, textDecorationLine: 'underline' },
    pressed: { opacity: 0.6 },
    sheetBody: { paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
  }),
);
