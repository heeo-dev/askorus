// The AIs as a sheet (S5c, B2-Many-Sheet): one 64 px row per AI with its
// avatar, name and model, and its state at the right (answered with the
// time, answering, failed). The Ask screen uses it to pick the AIs that
// answer and to show who answered a question. A picker row is the AI's
// switch (role 'switch' with its state, UX-9); other rows are buttons, or
// plain text when they do nothing.

import type { ReactNode } from 'react';
import { Pressable, StyleSheet, Text, View } from 'react-native';

import { Avatar } from './Avatar';
import { Spinner } from './controls';
import { Icon } from './Icon';
import { Sheet } from './Sheet';
import { themed, useTheme } from './theme';

export type AISheetRow = {
  agent: string;
  name: string;
  model?: string;
  status?: { tone: 'sub' | 'waiting' | 'danger'; text: string; icon?: 'check' | 'spinner' };
  onPress?: () => void;
  onLongPress?: () => void;
  right?: ReactNode;
  disabled?: boolean;
  /** Set on a row that turns the AI on or off: it is a switch, checked when on. */
  checked?: boolean;
  /** Always an object: a value that comes and goes stays behind on Android (R-40). */
  accessibilityValue: { text: string };
};

export function AISheet({ open, onClose, title, subtitle, rows }: { open: boolean; onClose: () => void; title: string; subtitle?: string; rows: AISheetRow[] }) {
  const th = useTheme();
  const s = styles(th);
  return (
    <Sheet open={open} onClose={onClose} title={title} subtitle={subtitle} testID="ai-sheet">
      <View style={s.list}>
        {rows.map((r, i) => {
          const tone = r.status?.tone === 'danger' ? th.danger : r.status?.tone === 'waiting' ? th.waitingText : th.textSub;
          const body = (
            <>
              <Avatar agent={r.agent} size={28} />
              <View style={s.names}>
                <Text style={s.name} numberOfLines={1}>
                  {r.name}
                </Text>
                {r.model ? (
                  <Text style={s.model} numberOfLines={1}>
                    {r.model}
                  </Text>
                ) : null}
              </View>
              {r.status ? (
                <View style={s.status}>
                  {r.status.icon === 'check' ? <Icon name="check" size={16} color={th.ok} /> : null}
                  {r.status.icon === 'spinner' ? <Spinner size={16} /> : null}
                  <Text style={[s.statusText, { color: tone }]} numberOfLines={1}>
                    {r.status.text}
                  </Text>
                </View>
              ) : null}
              {r.right}
              {r.onPress && !r.right ? <Icon name="chevronRight" size={18} color={th.caption} /> : null}
            </>
          );
          // The status is said once: left out of the label when the value already says it ('Answered, 9 s').
          const said = !!r.status?.text && r.accessibilityValue.text.endsWith(r.status.text);
          const label = [r.name, r.model, said ? undefined : r.status?.text].filter(Boolean).join(', ');
          const row = [s.row, i > 0 && s.divided, r.disabled && s.off];
          const toggles = r.checked !== undefined;
          const state = toggles ? { checked: r.checked, disabled: !!r.disabled } : { disabled: !!r.disabled };
          return r.onPress || r.onLongPress ? (
            <Pressable
              key={r.agent}
              onPress={r.onPress}
              onLongPress={r.onLongPress}
              disabled={r.disabled}
              accessibilityRole={toggles ? 'switch' : 'button'}
              accessibilityLabel={label}
              accessibilityState={state}
              accessibilityValue={r.accessibilityValue}
              style={({ pressed }) => [...row, pressed && s.pressed]}
            >
              {body}
            </Pressable>
          ) : (
            <View
              key={r.agent}
              style={row}
              accessible
              accessibilityRole={toggles ? 'switch' : undefined}
              accessibilityLabel={label}
              accessibilityState={toggles ? state : undefined}
              accessibilityValue={r.accessibilityValue}
            >
              {body}
            </View>
          );
        })}
      </View>
    </Sheet>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    list: { paddingTop: 4 },
    row: { minHeight: 64, flexDirection: 'row', alignItems: 'center', gap: 12, paddingLeft: 20, paddingRight: 12 },
    divided: { borderTopWidth: 1, borderTopColor: th.divider },
    off: { opacity: 0.45 },
    pressed: { backgroundColor: th.raised },
    names: { flex: 1, minWidth: 0, gap: 1 },
    name: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    model: { fontSize: 13, lineHeight: 18, color: th.caption },
    status: { flexDirection: 'row', alignItems: 'center', gap: 6, flexShrink: 1 },
    statusText: { fontSize: 14, lineHeight: 20, fontVariant: ['tabular-nums'] },
  }),
);
