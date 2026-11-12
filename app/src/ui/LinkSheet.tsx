// The real address of a link in an answer, before it opens (#47). Shown
// after a long press on any link, and after a tap when the link's words
// name another site than the one it goes to.

import * as Clipboard from 'expo-clipboard';
import { useEffect, useState } from 'react';
import { Linking, StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';

import { Button } from './Button';
import { linkHost } from './linkCheck';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

/** A link somebody pressed: where it goes, and the other site its words name, if any. */
export type LinkAsk = { href: string; shown: string | null };

export function LinkSheet({ link, onClose }: { link: LinkAsk | null; onClose: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const [copied, setCopied] = useState(false);
  useEffect(() => setCopied(false), [link]);
  if (!link) return null;
  const host = linkHost(link.href) ?? link.href;

  const open = () => {
    onClose();
    Linking.openURL(link.href).catch(() => undefined);
  };
  const copy = async () => {
    await Clipboard.setStringAsync(link.href);
    setCopied(true);
  };

  return (
    <Sheet open onClose={onClose} title={t('link.title')} testID="link-sheet">
      <View style={s.body}>
        {link.shown ? (
          <Text style={s.warn} accessibilityRole="alert">
            {t('link.warn', { shown: link.shown, host })}
          </Text>
        ) : null}
        <View style={s.where}>
          <Text style={s.caption}>{t('link.goesTo')}</Text>
          <Text style={s.host} selectable>
            {host}
          </Text>
          <Text style={s.address} selectable numberOfLines={6}>
            {link.href}
          </Text>
        </View>
        <Button icon="open" label={t('link.open')} onPress={open} />
        <Button variant="outline" icon="copy" label={copied ? t('sheet.copied') : t('link.copy')} onPress={copy} />
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { gap: 12, paddingTop: 6, paddingHorizontal: 16, paddingBottom: 8 },
    warn: { fontSize: 14, lineHeight: 20, color: th.danger },
    where: { gap: 4, paddingBottom: 6 },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    host: { fontSize: 18, lineHeight: 25, fontWeight: '600', color: th.text },
    address: { fontSize: 13, lineHeight: 19.5, color: th.textSub },
  }),
);
