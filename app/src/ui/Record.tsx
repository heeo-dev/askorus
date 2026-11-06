// The conversation record on the phone (#29). Under a follow-up that
// carried it, a small '앞 결론을 이어받음 ›' that opens what was sent: the
// points earlier debates settled, and whether the last final answer went
// too. Before the first follow-up that would carry one, a notice above the
// follow-up bar says where it goes (once; '이번만 빼고 보내기' sends the
// question alone).

import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import type { RequestContext } from '@/api/types';
import { plainNames } from '@/ask/aiNames';
import { resultLabel } from '@/ask/debate';
import { t } from '@/i18n';

import { Button } from './Button';
import { Icon } from './Icon';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

/** RecordLine is the line under a question that carried the record; null when it carried none. */
export function RecordLine({ context, onOpen }: { context?: RequestContext; onOpen: () => void }) {
  const th = useTheme();
  const s = styles(th);
  if (!context) return null;
  return (
    <View style={s.row}>
      <Pressable
        onPress={onOpen}
        accessibilityRole="button"
        accessibilityLabel={t('ask.record.lineLabel')}
        hitSlop={{ top: 8, bottom: 8 }}
        style={({ pressed }) => [s.chip, pressed && { opacity: 0.7 }]}
        testID="record-line"
      >
        <Text style={s.chipText}>{t('ask.record.line')}</Text>
        <Icon name="chevronRight" size={14} color={th.caption} />
      </Pressable>
    </View>
  );
}

/** RecordSheet shows the record a question carried. */
export function RecordSheet({ context, onClose }: { context: RequestContext | null; onClose: () => void }) {
  const s = styles(useTheme());
  const settled = context?.settled ?? [];
  return (
    <Sheet open={!!context} onClose={onClose} title={t('ask.record.title')} testID="record-sheet">
      {context ? (
        <ScrollView style={s.body} contentContainerStyle={s.bodyIn}>
          {settled.length ? (
            <View style={s.list}>
              <Text style={s.label} accessibilityRole="header">
                {t('ask.record.settled')}
              </Text>
              {settled.map((p, i) => (
                <View key={`${i}-${p.topic}`} style={s.point}>
                  <View style={s.pointHead}>
                    <Text style={s.topic}>{plainNames(p.topic)}</Text>
                    {resultLabel(p.result) ? <Text style={s.result}>{resultLabel(p.result)}</Text> : null}
                  </View>
                  <Text style={s.line} selectable>
                    {plainNames(p.line)}
                  </Text>
                </View>
              ))}
            </View>
          ) : null}
          {context.last ? <Text style={s.note}>{t('ask.record.last')}</Text> : null}
          <Text style={s.note}>{t('ask.record.how')}</Text>
        </ScrollView>
      ) : null}
    </Sheet>
  );
}

/** RecordNotice says, once, where the record goes, above the follow-up bar. */
export function RecordNotice({ onGo, onWithout }: { onGo: () => void; onWithout: () => void }) {
  const s = styles(useTheme());
  return (
    <View style={s.notice} accessibilityLiveRegion="polite" testID="record-notice">
      <Text style={s.noticeTitle} accessibilityRole="header">
        {t('ask.record.notice.title')}
      </Text>
      <Text style={s.noticeBody}>{t('ask.record.notice.body')}</Text>
      <View style={s.noticeActions}>
        <Button label={t('ask.record.notice.go')} onPress={onGo} />
        <Button variant="quiet" label={t('ask.record.notice.without')} onPress={onWithout} />
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    row: { flexDirection: 'row', justifyContent: 'flex-end' },
    chip: { flexDirection: 'row', alignItems: 'center', gap: 2, paddingVertical: 2, paddingLeft: 8, paddingRight: 4 },
    chipText: { fontSize: 13, lineHeight: 18, color: th.caption },
    body: { maxHeight: 520 },
    bodyIn: { gap: 12, paddingTop: 4, paddingHorizontal: 20, paddingBottom: 8 },
    list: { gap: 10 },
    label: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    point: { gap: 4, borderRadius: 12, padding: 12, backgroundColor: th.raised },
    pointHead: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: 8 },
    topic: { flexShrink: 1, fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    result: { fontSize: 12, lineHeight: 16, color: th.caption },
    line: { fontSize: 14, lineHeight: 21, color: th.text },
    note: { fontSize: 13, lineHeight: 19.5, color: th.textSub },
    notice: { gap: 8, marginHorizontal: 16, marginBottom: 8, padding: 12, borderRadius: 12, backgroundColor: th.soft, borderWidth: 1, borderColor: th.softLine },
    noticeTitle: { fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    noticeBody: { fontSize: 14, lineHeight: 21, color: th.textSub },
    noticeActions: { flexDirection: 'row', flexWrap: 'wrap', gap: 8 },
  }),
);
