// Settings > Tailscale (#57): whether this phone is signed in to the
// person's Tailscale network, as which account and under which name, and
// signing in or out. Tailscale is inside the app; this is all there is to
// set of it.

import { StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';
import { useTailnet } from '@/tailnet/node';

import { Button } from './Button';
import { Sheet } from './Sheet';
import { TailnetAboutText } from './TailnetAbout';
import { confirmTailnetLogout, TailnetLogin } from './TailnetLogin';
import { themed, useTheme } from './theme';

/** The day a sign-in ends, in the phone's own date form. */
function day(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : d.toLocaleDateString();
}

export function TailnetSheet({ open, onClose }: { open: boolean; onClose: () => void }) {
  return (
    <Sheet open={open} onClose={onClose} title={t('tailnet.title')} testID="tailnet-sheet">
      {open ? <Body onClose={onClose} /> : null}
    </Sheet>
  );
}

function Body({ onClose }: { onClose: () => void }) {
  const s = styles(useTheme());
  const st = useTailnet();
  const signedIn = st.state === 'running' || st.state === 'needsApproval';
  const logout = () => confirmTailnetLogout(onClose);
  return (
    <View style={s.body}>
      <TailnetLogin about={false} />
      {st.state === 'running' && st.name ? (
        <View style={s.fact}>
          <Text style={s.label}>{t('tailnet.thisPhone')}</Text>
          <Text style={s.value} selectable>
            {st.name}
          </Text>
        </View>
      ) : null}
      {st.state === 'running' && st.keyExpiry ? <Text style={s.caption}>{t('tailnet.expires', { date: day(st.keyExpiry) })}</Text> : null}
      {signedIn ? <Button variant="outline" label={t('tailnet.logout')} onPress={logout} /> : null}
      <View style={s.about}>
        <TailnetAboutText />
      </View>
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    body: { gap: 14, paddingTop: 6, paddingHorizontal: 20, paddingBottom: 8 },
    fact: { gap: 2 },
    about: { paddingTop: 14, borderTopWidth: StyleSheet.hairlineWidth, borderTopColor: th.divider },
    label: { fontSize: 13, lineHeight: 18, color: th.caption },
    value: { fontSize: 15, lineHeight: 21, color: th.text },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
  }),
);
