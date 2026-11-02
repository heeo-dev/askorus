// What new sessions start with (S5c, Settings › 새 세션 기본값): a model and
// an effort, each a row of choices in a sheet. Empty is Claude's own
// default, so nothing is sent.

import { ScrollView, StyleSheet, View } from 'react-native';

import { efforts, models, type Prefs } from '@/connections/prefs';
import { t } from '@/i18n';

import { Segment } from './controls';
import { SectionLabel } from './List';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

type Model = (typeof models)[number];
type Effort = (typeof efforts)[number];

/** settingsLine is a model and effort in one line: 'Default', or 'opus · high'. */
export function settingsLine(model: string, effort: string): string {
  return model || effort ? [model, effort].filter(Boolean).join(' · ') : t('new.default');
}

/** knownModel keeps a saved model only if it is one this app offers. */
export const knownModel = (m: string): Model => ((models as readonly string[]).includes(m) ? (m as Model) : '');

export function DefaultsSheet({ open, onClose, prefs, onChange }: { open: boolean; onClose: () => void; prefs: Prefs; onChange: (p: Prefs) => void }) {
  const s = styles(useTheme());
  return (
    <Sheet open={open} onClose={onClose} title={t('settings.defaults')} testID="defaults-sheet">
      <View style={s.body}>
        <View style={s.field}>
          <SectionLabel text={t('new.model')} />
          <ScrollView horizontal showsHorizontalScrollIndicator={false}>
            <Segment
              options={models}
              value={knownModel(prefs.model)}
              onChange={(model) => onChange({ ...prefs, model })}
              labelOf={(m) => m || t('new.default')}
              look="tier"
              label={t('new.model')}
            />
          </ScrollView>
        </View>
        <View style={s.field}>
          <SectionLabel text={t('new.effort')} />
          <ScrollView horizontal showsHorizontalScrollIndicator={false}>
            <Segment
              options={efforts}
              value={prefs.effort as Effort}
              onChange={(effort) => onChange({ ...prefs, effort })}
              labelOf={(e) => e || t('new.default')}
              look="tier"
              label={t('new.effort')}
            />
          </ScrollView>
        </View>
      </View>
    </Sheet>
  );
}

const styles = themed(() =>
  StyleSheet.create({
    body: { gap: 18, paddingTop: 10, paddingHorizontal: 16, paddingBottom: 8 },
    field: { gap: 8 },
  }),
);
