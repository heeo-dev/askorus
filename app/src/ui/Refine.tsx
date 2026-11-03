// Question refining on the phone (질문 정제; milestone C, design C-4). Under
// the question bubble: 'Refining the question…' while the moderator reads
// it, then a small 'Answer format: Comparison ›' that opens what was sent
// (the kind, depth, intent and the template every AI got after the
// question, which itself went as written), or 'Sent without refining' when
// refining failed. Nothing shows when the run did not refine.

import { Pressable, ScrollView, StyleSheet, Text, View } from 'react-native';

import { agentName } from '@/api/agents';
import type { Refine } from '@/api/types';
import { displayModel } from '@/ask/present';
import { has, type MessageKey, t } from '@/i18n';

import { Icon } from './Icon';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

/** kindName is a kind as people read it; an unknown one as the PC wrote it. */
export function kindName(kind: string): string {
  const key = `ask.refine.kind.${kind}` as MessageKey;
  return has(key) ? t(key) : kind;
}

function depthName(depth: string): string {
  const key = `ask.refine.depth.${depth}` as MessageKey;
  return has(key) ? t(key) : depth;
}

/** RefineLine is the line under a question; null when the run did not refine. */
export function RefineLine({ refine, onOpen }: { refine?: Refine; onOpen: () => void }) {
  const th = useTheme();
  const s = styles(th);
  if (!refine) return null;
  if (refine.status === 'running') {
    return (
      <Text style={s.caption} testID="refine-running">
        {t('ask.refine.running')}
      </Text>
    );
  }
  if (refine.status === 'done' && refine.spec) {
    const kind = kindName(refine.spec.kind);
    return (
      <View style={s.row}>
        <Pressable
          onPress={onOpen}
          accessibilityRole="button"
          accessibilityLabel={t('ask.refine.chipLabel', { kind })}
          hitSlop={{ top: 8, bottom: 8 }}
          style={({ pressed }) => [s.chip, pressed && { opacity: 0.7 }]}
        >
          <Text style={s.chipText}>{t('ask.refine.chip', { kind })}</Text>
          <Icon name="chevronRight" size={14} color={th.caption} />
        </Pressable>
      </View>
    );
  }
  return (
    <Text style={s.caption} testID="refine-none">
      {t('ask.refine.none')}
    </Text>
  );
}

/** RefineSheet shows what refining sent with a question. */
export function RefineSheet({ refine, onClose }: { refine: Refine | null; onClose: () => void }) {
  const s = styles(useTheme());
  const spec = refine?.spec;
  return (
    <Sheet open={!!(refine && spec)} onClose={onClose} title={t('ask.refine.title')} testID="refine-sheet">
      {refine && spec ? (
        <ScrollView style={s.body} contentContainerStyle={s.bodyIn}>
          <View style={s.facts}>
            <Fact label={t('ask.refine.kindLabel')} value={kindName(spec.kind)} />
            <Fact label={t('ask.refine.depthLabel')} value={depthName(spec.depth)} />
            <Fact label={t('ask.refine.intentLabel')} value={spec.intent} />
          </View>
          {refine.template ? (
            <View style={s.template}>
              <Text style={s.templateText} selectable>
                {refine.template}
              </Text>
            </View>
          ) : (
            <Text style={s.note}>{t('ask.refine.noTemplate')}</Text>
          )}
          <Text style={s.note}>{t('ask.refine.how')}</Text>
          {refine.agent ? (
            <Text style={s.by}>
              {t('ask.refine.by', {
                name: agentName(refine.agent),
                model: displayModel(refine.model),
                s: ((refine.ms ?? 0) / 1000).toFixed(1),
              })}
            </Text>
          ) : null}
        </ScrollView>
      ) : null}
    </Sheet>
  );
}

function Fact({ label, value }: { label: string; value: string }) {
  const s = styles(useTheme());
  return (
    <View style={s.fact}>
      <Text style={s.factLabel}>{label}</Text>
      <Text style={s.factValue}>{value}</Text>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    row: { flexDirection: 'row', justifyContent: 'flex-end' },
    chip: { flexDirection: 'row', alignItems: 'center', gap: 2, paddingVertical: 2, paddingLeft: 8, paddingRight: 4 },
    chipText: { fontSize: 13, lineHeight: 18, color: th.caption },
    caption: { alignSelf: 'flex-end', fontSize: 13, lineHeight: 18, color: th.caption },
    body: { maxHeight: 520 },
    bodyIn: { gap: 12, paddingTop: 4, paddingHorizontal: 20, paddingBottom: 8 },
    facts: { gap: 8 },
    fact: { flexDirection: 'row', gap: 12 },
    factLabel: { width: 48, fontSize: 14, lineHeight: 20, color: th.caption },
    factValue: { flex: 1, fontSize: 14, lineHeight: 20, color: th.text },
    template: { borderRadius: 12, padding: 12, backgroundColor: th.raised },
    templateText: { fontSize: 14, lineHeight: 21, color: th.text },
    note: { fontSize: 13, lineHeight: 19.5, color: th.textSub },
    by: { fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
