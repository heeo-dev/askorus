// What else can be done with an answer, behind the '...' at the end of its
// actions (#50): share it, or report it.
// Share hands the answer's text (what Copy gives) to Android's share window,
// with nothing added.
// Report writes a report and opens the person's mail app with it (see
// ask/report): the sheet asks what kind of problem it is and whether the
// question and the answer go along (off unless turned on, and shown before
// they do). The app sends nothing itself. Where no mail app opens, the
// report can be copied, with the address to send it to.

import * as Clipboard from 'expo-clipboard';
import Constants from 'expo-constants';
import { useState } from 'react';
import { Linking, Platform, Share, StyleSheet, Text, View } from 'react-native';

import { mailLink, mailText, reportAddress, reportCategories, type ReportCategory, reportMail, reportQuoted, type ReportSubject } from '@/ask/report';
import { t } from '@/i18n';

import { Button } from './Button';
import { SwitchRow } from './controls';
import { Icon } from './Icon';
import { Group, Row, SectionLabel } from './List';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

/**
 * AnswerMore is the open sheet (mount it only while it is open, so each
 * opening starts at the menu). onSaid gets a line to show under the answer
 * once the mail app has been opened.
 */
export function AnswerMore({ subject, onClose, onSaid }: { subject: ReportSubject; onClose: () => void; onSaid: (text: string) => void }) {
  const th = useTheme();
  const s = styles(th);
  const [view, setView] = useState<'menu' | 'report'>('menu');
  const [category, setCategory] = useState<ReportCategory | null>(null);
  const [withText, setWithText] = useState(false);
  const [noMail, setNoMail] = useState(false);
  const [copied, setCopied] = useState(false);

  const share = () => {
    onClose();
    Share.share({ message: subject.text }).catch(() => undefined);
  };

  const mail = () =>
    reportMail(subject, {
      category: category ?? 'other',
      withText,
      version: Constants.expoConfig?.version ?? '',
      os: Platform.OS === 'ios' ? 'iOS' : 'Android',
    });

  const openMail = async () => {
    try {
      await Linking.openURL(mailLink(mail()));
    } catch {
      setNoMail(true);
      return;
    }
    onSaid(t('report.sendInMail'));
    onClose();
  };

  const copy = async () => {
    await Clipboard.setStringAsync(mailText(mail()));
    setCopied(true);
  };

  if (view === 'menu') {
    return (
      <Sheet open onClose={onClose} title={t('ask.more')} testID="answer-more">
        <View style={s.body}>
          <Group>
            <Row title={t('ask.share')} left={<Icon name="share" size={20} color={th.textSub} />} onPress={share} />
            <Row title={t('ask.report')} left={<Icon name="flag" size={20} color={th.textSub} />} chevron onPress={() => setView('report')} />
          </Group>
        </View>
      </Sheet>
    );
  }

  return (
    <Sheet open onClose={onClose} title={t('report.title')} subtitle={subject.ai} testID="answer-report">
      <View style={s.body}>
        <Text style={s.note}>{t('report.note')}</Text>
        <SectionLabel text={t('report.category')} />
        <Group>
          {reportCategories.map((c) => {
            const on = c === category;
            const title = t(`report.category.${c}`);
            return (
              <Row
                key={c}
                title={title}
                onPress={() => setCategory(c)}
                minHeight={48}
                accessibilityRole="radio"
                accessibilityLabel={title}
                accessibilityState={{ checked: on }}
                titleStyle={on ? s.on : undefined}
                right={on ? <Icon name="check" size={18} color={th.accent} /> : <View style={s.space} />}
              />
            );
          })}
        </Group>
        <SwitchRow label={t('report.withText')} value={withText} onChange={setWithText} />
        {withText ? (
          <View style={s.quoted}>
            <Text style={s.caption}>{t('report.withTextOn')}</Text>
            <Text style={s.quote} numberOfLines={10} testID="report-quoted">
              {reportQuoted(subject)}
            </Text>
          </View>
        ) : (
          <Text style={s.caption}>{t('report.withTextOff')}</Text>
        )}
        <Button label={t('report.openMail')} onPress={openMail} disabled={!category} />
        {noMail ? (
          <View style={s.fallback}>
            <Text style={s.error} selectable>
              {t('report.noMail', { address: reportAddress })}
            </Text>
            <Button variant="outline" icon="copy" label={copied ? t('sheet.copied') : t('report.copy')} onPress={copy} />
          </View>
        ) : null}
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { gap: 12, paddingTop: 6, paddingHorizontal: 16, paddingBottom: 8 },
    note: { fontSize: 14, lineHeight: 20, color: th.textSub },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    on: { fontWeight: '600', color: th.text },
    space: { width: 18 },
    quoted: { gap: 6 },
    quote: { fontSize: 13, lineHeight: 19.5, color: th.textSub, padding: 12, borderRadius: 12, backgroundColor: th.raised },
    fallback: { gap: 10 },
    error: { fontSize: 14, lineHeight: 20, color: th.danger },
  }),
);
