// Contacting the project from inside the app (#55): a question, an idea
// or a bug, as opposed to reporting an AI's answer (#50, ui/AnswerMore).
// Askorus has no server to receive a message, so there are two ways out:
// the person's own mail app, with the address and the app's version
// written, or a new issue on GitHub, which is public and needs an account.
// The app sends nothing by itself.

import * as Clipboard from 'expo-clipboard';
import Constants from 'expo-constants';
import { useEffect, useState } from 'react';
import { Linking, Platform, StyleSheet, Text, View } from 'react-native';

import { mailLink, reportAddress, type ReportMail } from '@/ask/report';
import { t } from '@/i18n';

import { Button } from './Button';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

/** Where a new issue is written: the forms for a bug and an idea. */
export const issuesUrl = 'https://github.com/heeo-dev/askorus/issues/new/choose';

/** contactMail is the mail the app writes: who it goes to, a subject to fill in, and the app's version. */
export function contactMail(o: { version: string; os: string }): ReportMail {
  return {
    to: reportAddress,
    subject: t('contact.mail.subject'),
    body: `${t('contact.mail.write')}\n\n\n${t('report.mail.app', { version: o.version || '?', os: o.os })}\n`,
  };
}

export function ContactSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  const th = useTheme();
  const s = styles(th);
  const [noMail, setNoMail] = useState(false);
  const [copied, setCopied] = useState(false);
  useEffect(() => {
    if (open) {
      setNoMail(false);
      setCopied(false);
    }
  }, [open]);
  if (!open) return null;

  const openMail = async () => {
    const mail = contactMail({ version: Constants.expoConfig?.version ?? '', os: Platform.OS === 'ios' ? 'iOS' : 'Android' });
    try {
      await Linking.openURL(mailLink(mail));
    } catch {
      setNoMail(true);
      return;
    }
    onClose();
  };
  const copy = async () => {
    await Clipboard.setStringAsync(reportAddress);
    setCopied(true);
  };
  const openIssues = () => {
    onClose();
    Linking.openURL(issuesUrl).catch(() => undefined);
  };

  return (
    <Sheet open onClose={onClose} title={t('contact.title')} testID="contact-sheet">
      <View style={s.body}>
        <Text style={s.note}>{t('contact.note')}</Text>
        <View style={s.way}>
          <Button icon="send" label={t('contact.mail')} onPress={openMail} />
          <Text style={s.caption}>{t('contact.mailHint')}</Text>
          {noMail ? (
            <View style={s.fallback}>
              <Text style={s.error} selectable>
                {t('contact.noMail', { address: reportAddress })}
              </Text>
              <Button variant="outline" icon="copy" label={copied ? t('sheet.copied') : t('contact.copy')} onPress={copy} />
            </View>
          ) : null}
        </View>
        <View style={s.way}>
          <Button variant="outline" icon="open" label={t('contact.github')} onPress={openIssues} />
          <Text style={s.caption}>{t('contact.githubHint')}</Text>
        </View>
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { gap: 18, paddingTop: 6, paddingHorizontal: 16, paddingBottom: 8 },
    note: { fontSize: 14, lineHeight: 20, color: th.textSub },
    way: { gap: 8 },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    fallback: { gap: 10 },
    error: { fontSize: 14, lineHeight: 20, color: th.danger },
  }),
);
