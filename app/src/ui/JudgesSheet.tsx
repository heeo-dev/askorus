// 판정 정보 / Judging details (S5c, F-Sheet-Judges), opened from the info
// button of the combined answer or the evidence: who wrote the combined
// answer (its model and time, or how it stands), then every judging
// request under the AI that judged it ('Claude → Codex' with its time, its
// state, or why it failed in the AI's words), and the notes: that it can
// be wrong, which companies get the answers, and whether the judging has
// been measured. These lines were under the answer before S5c.

import { StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { Comparison, Info } from '@/api/types';
import { judgeModels, synthErrorText, synthOn } from '@/ask/compare';
import { displayModel, judgedCount, judgeGroups, metaLine, tenths } from '@/ask/present';
import type { Turn } from '@/ask/thread';
import { t } from '@/i18n';

import { Avatar } from './Avatar';
import { Icon, type IconName } from './Icon';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

export function JudgesSheet({ turn, snap, info, onClose }: { turn: Turn | null; snap?: Comparison; info: Info | null; onClose: () => void }) {
  const th = useTheme();
  const s = styles(th);
  if (!turn) return null;
  const meta = metaLine(turn);
  const y = snap?.synthesis && snap.synthesis.status !== 'off' ? snap.synthesis : undefined;
  const writing = synthOn(turn);
  const groups = snap ? judgeGroups(snap, turn.agents) : [];
  const planned = snap ? [] : judgeModels(turn, info);

  const notes: Array<{ icon: IconName; text: string }> = [];
  if (writing) notes.push({ icon: 'alert', text: t('compare.synth.mayErr') });
  notes.push({ icon: 'open', text: t('compare.footer.company') });
  if (writing) notes.push({ icon: 'open', text: t('compare.synth.company') });
  if (!info?.judging?.verified) notes.push({ icon: 'info', text: t('compare.footer.unverified') });
  notes.push({ icon: 'alert', text: t('compare.footer.mayErr') });

  const synthStatus = (() => {
    if (!y) return null;
    switch (y.status) {
      case 'done':
        return { text: y.ms !== undefined ? tenths(y.ms) : '' };
      case 'waiting':
        return { text: t('compare.req.queued') };
      case 'running':
        return { text: t('compare.synth.writing') };
      case 'failed':
        return { text: synthErrorText(y.error?.code || 'internal', y.error?.message), failed: true };
      default:
        return { text: y.status };
    }
  })();

  return (
    <Sheet open onClose={onClose} title={t('compare.info')} subtitle={meta || undefined} testID="judges-sheet">
      {y && synthStatus ? (
        <View>
          <Text style={s.label} accessibilityRole="header">
            {t('compare.synth.writtenBy')}
          </Text>
          <View style={s.writer} accessible accessibilityLabel={[y.agent ? agentName(y.agent) : t('compare.synth.title'), displayModel(y.model), synthStatus.text].filter(Boolean).join(', ')}>
            {y.agent ? <Avatar agent={y.agent} /> : null}
            <Text style={s.writerName}>{y.agent ? agentName(y.agent) : t('compare.synth.title')}</Text>
            {y.model ? (
              <Text style={s.model} numberOfLines={1}>
                {displayModel(y.model)}
              </Text>
            ) : null}
            {!synthStatus.failed && synthStatus.text ? <Text style={s.writerTime}>{synthStatus.text}</Text> : null}
          </View>
          {synthStatus.failed ? <Text style={s.failedLine}>{synthStatus.text}</Text> : null}
        </View>
      ) : null}

      {snap ? (
        <View style={y ? s.divided : undefined}>
          <View style={s.labelRow}>
            <Text style={s.labelText} accessibilityRole="header">
              {t('compare.judgeCount', { n: judgedCount(snap) })}
            </Text>
            <Text style={s.direction} importantForAccessibility="no" accessibilityElementsHidden>
              {t('compare.direction')}
            </Text>
          </View>
          <Text style={s.help}>{t('compare.judgeCountHelp')}</Text>
          {groups.map((g, gi) => (
            <View key={g.key} style={[s.group, gi > 0 && s.groupLine]}>
              {g.agent ? (
                <View style={s.groupHead}>
                  <Avatar agent={g.agent} />
                  {/* One Text with the model nested: on Android a bold name beside
                      a second Text could wrap inside its own measured width
                      ('Judged by' / 'Codex', S5c check), and one text lays out
                      as it measures. */}
                  <Text
                    style={s.groupName}
                    accessibilityRole="header"
                    accessibilityLabel={[t('compare.judgeGroup', { name: g.name }), g.model].filter(Boolean).join(', ')}
                  >
                    {t('compare.judgeGroup', { name: g.name })}
                    {g.model ? <Text style={s.groupModel}>{`  ${g.model}`}</Text> : null}
                  </Text>
                </View>
              ) : null}
              {g.rows.map((r) => (
                <View key={r.id} style={[s.reqRow, !g.agent && s.reqRowPlain]} accessible accessibilityLabel={r.a11y}>
                  <View style={s.reqLine}>
                    <Text style={s.reqText}>{r.text}</Text>
                    {!r.failed && r.status ? <Text style={s.reqStatus}>{r.status}</Text> : null}
                  </View>
                  {r.failed ? <Text style={s.reqFailed}>{r.status}</Text> : null}
                </View>
              ))}
            </View>
          ))}
        </View>
      ) : planned.length ? (
        <View>
          <Text style={s.label} accessibilityRole="header">
            {t('compare.judgesTitle')}
          </Text>
          {planned.map((j) => (
            <View key={j.agent} style={s.writer}>
              <Text style={s.writerName}>{j.agent}</Text>
              {j.model ? <Text style={s.model}>{displayModel(j.model)}</Text> : null}
            </View>
          ))}
        </View>
      ) : null}

      <View style={s.notes}>
        {notes.map((n, i) => (
          <View key={i} style={s.note}>
            <Icon name={n.icon} size={18} color={th.textSub} style={s.noteIcon} />
            <Text style={s.noteText}>{n.text}</Text>
          </View>
        ))}
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    label: { minHeight: 32, paddingHorizontal: 20, fontSize: 13, fontWeight: '500', lineHeight: 32, color: th.caption },
    labelRow: { minHeight: 32, flexDirection: 'row', alignItems: 'center', paddingHorizontal: 20 },
    labelText: { fontSize: 13, fontWeight: '500', lineHeight: 18, color: th.caption },
    direction: { marginLeft: 'auto', fontSize: 13, lineHeight: 18, color: th.caption },
    help: { paddingHorizontal: 20, paddingBottom: 8, fontSize: 13, lineHeight: 19.5, color: th.textSub },
    divided: { marginTop: 4, paddingTop: 4, borderTopWidth: 1, borderTopColor: th.divider },
    writer: { minHeight: 48, flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: 20 },
    writerName: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    model: { flexShrink: 1, fontSize: 13, lineHeight: 18, color: th.caption },
    writerTime: { marginLeft: 'auto', fontSize: 14, lineHeight: 20, color: th.textSub, fontVariant: ['tabular-nums'] },
    failedLine: { paddingHorizontal: 20, paddingBottom: 8, fontSize: 13, lineHeight: 18.2, color: th.danger },
    group: { paddingTop: 2, paddingBottom: 6 },
    groupLine: { borderTopWidth: 1, borderTopColor: th.divider },
    groupHead: { minHeight: 38, flexDirection: 'row', alignItems: 'center', gap: 10, paddingHorizontal: 20 },
    groupName: { flex: 1, fontSize: 14, fontWeight: '600', lineHeight: 20, color: th.text },
    groupModel: { fontSize: 13, fontWeight: '400', color: th.caption },
    reqRow: { minHeight: 32, justifyContent: 'center', paddingLeft: 54, paddingRight: 20, paddingVertical: 4 },
    reqRowPlain: { paddingLeft: 20 },
    reqLine: { flexDirection: 'row', alignItems: 'center', gap: 10 },
    reqText: { flexShrink: 1, fontSize: 14, lineHeight: 20, color: th.textSub },
    reqStatus: { marginLeft: 'auto', fontSize: 13, lineHeight: 18, color: th.caption, fontVariant: ['tabular-nums'] },
    reqFailed: { fontSize: 13, lineHeight: 18.2, color: th.danger },
    notes: { marginTop: 8, marginHorizontal: 16, paddingVertical: 12, paddingHorizontal: 14, borderRadius: 14, backgroundColor: th.raised, gap: 10 },
    note: { flexDirection: 'row', alignItems: 'flex-start', gap: 10 },
    noteIcon: { marginTop: 1 },
    noteText: { flex: 1, fontSize: 13.5, lineHeight: 20.25, color: th.textSub },
  }),
);
