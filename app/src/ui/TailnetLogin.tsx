// The app's Tailscale sign-in (#57): shows whether this phone is signed in
// and, when it is not, a button that opens the sign-in page in the browser.
// The link comes from the app's own node a moment after the button is
// pressed; the app notices by itself when the person is back and signed in.
// Used on the pairing screen (its first step), in Settings and where a PC
// cannot be reached because the phone is not signed in.

import { useEffect, useRef, useState } from 'react';
import { Alert, Linking, Pressable, StyleSheet, Text, View } from 'react-native';

import { t } from '@/i18n';
import { loginTailnet, logoutTailnet, type TailnetStatus, useTailnet } from '@/tailnet/node';

import { Button } from './Button';
import { Spinner } from './controls';
import { Icon } from './Icon';
import { TailnetAboutLink } from './TailnetAbout';
import { themed, useTheme } from './theme';

/**
 * Asks, then signs this phone out of Tailscale. Signing out is also how
 * the account is changed: the next sign-in can be with another one.
 */
export function confirmTailnetLogout(after?: () => void) {
  Alert.alert(t('tailnet.logoutTitle'), t('tailnet.logoutBody'), [
    { text: t('sheet.cancel'), style: 'cancel' },
    {
      text: t('tailnet.logoutDo'),
      style: 'destructive',
      onPress: () => {
        void logoutTailnet().finally(() => after?.());
      },
    },
  ]);
}

/** One line for a row: who is signed in, or that nobody is. */
export function tailnetLine(st: TailnetStatus): string {
  switch (st.state) {
    case 'running':
      return st.owner ?? t('tailnet.connected');
    case 'starting':
      return t('tailnet.starting');
    case 'needsApproval':
      return t('tailnet.waiting');
    case 'unavailable':
      return '';
    default:
      return t('tailnet.off');
  }
}

/**
 * onSignedIn is called when the sign-in goes through while the card is
 * showing, so what was waiting for it (a request that failed, a screen)
 * goes on without another press. about (on unless told otherwise) puts
 * the line that explains Tailscale above the button; a sheet that shows
 * the explanation itself turns it off. manage adds, under the account, the
 * way to sign out or change it: where a person finds out that they signed
 * in with the wrong one (pairing, a PC that is not in the account).
 */
export function TailnetLogin({ onSignedIn, about = true, manage = false }: { onSignedIn?: () => void; about?: boolean; manage?: boolean } = {}) {
  const th = useTheme();
  const s = styles(th);
  const st = useTailnet();
  const was = useRef(st.state);
  useEffect(() => {
    const before = was.current;
    was.current = st.state;
    if (before !== 'running' && st.state === 'running') onSignedIn?.();
  }, [st.state, onSignedIn]);
  const [asked, setAsked] = useState(false); // the person pressed the button
  const [error, setError] = useState('');
  const opened = useRef(''); // the link already handed to the browser

  // The link arrives a moment after the button: open it once.
  useEffect(() => {
    if (asked && st.state === 'needsLogin' && st.loginURL && opened.current !== st.loginURL) {
      opened.current = st.loginURL;
      Linking.openURL(st.loginURL).catch(() => undefined);
    }
    if (st.state === 'running') setAsked(false);
  }, [asked, st]);

  const login = async () => {
    setError('');
    setAsked(true);
    try {
      await loginTailnet();
    } catch {
      setAsked(false);
      setError(t('tailnet.loginFailed'));
    }
  };

  if (st.state === 'unavailable') {
    return (
      <View style={s.box} testID="tailnet-unavailable">
        <Text style={s.text}>{t('tailnet.unavailable')}</Text>
      </View>
    );
  }

  if (st.state === 'running') {
    return (
      <View style={s.box} testID="tailnet-running">
        <View style={s.row} accessible accessibilityLabel={`${t('tailnet.connected')} ${st.owner ?? ''}`}>
          <Icon name="check" size={18} color={th.ok} />
          <View style={s.grow}>
            <Text style={s.title}>{t('tailnet.connected')}</Text>
            {st.owner ? <Text style={s.caption}>{st.owner}</Text> : null}
          </View>
        </View>
        {manage ? (
          <Pressable
            onPress={() => confirmTailnetLogout()}
            accessibilityRole="button"
            accessibilityLabel={t('tailnet.logout')}
            hitSlop={10}
            style={({ pressed }) => [s.manage, pressed && s.pressed]}
          >
            <Text style={s.manageText}>{t('tailnet.logout')}</Text>
          </Pressable>
        ) : null}
      </View>
    );
  }

  if (st.state === 'needsApproval') {
    return (
      <View style={s.box} testID="tailnet-approval">
        <Text style={s.text}>{t('tailnet.needsApproval')}</Text>
      </View>
    );
  }

  const waiting = asked || st.state === 'starting';
  const link = st.state === 'needsLogin' ? st.loginURL : undefined;
  return (
    <View style={s.box} testID="tailnet-login">
      <Text style={s.lead}>{t('tailnet.why')}</Text>
      {about ? <TailnetAboutLink /> : null}
      <Text style={s.text}>{t('tailnet.loginBody')}</Text>
      {waiting ? (
        <View style={s.row}>
          <Spinner size={18} />
          <Text style={[s.caption, s.grow]}>{link ? t('tailnet.loginHint') : t('tailnet.starting')}</Text>
        </View>
      ) : null}
      {error ? <Text style={s.error}>{error}</Text> : null}
      {waiting && link ? (
        <Button variant="outline" icon="open" label={t('tailnet.openAgain')} onPress={() => Linking.openURL(link).catch(() => undefined)} />
      ) : (
        <Button icon="open" label={t('tailnet.login')} onPress={login} disabled={waiting} />
      )}
    </View>
  );
}

const styles = themed((th) =>
  StyleSheet.create({
    box: { gap: 12, padding: 16, borderRadius: 14, backgroundColor: th.surface },
    row: { flexDirection: 'row', alignItems: 'center', gap: 10 },
    grow: { flex: 1, minWidth: 0 },
    title: { fontSize: 15, fontWeight: '500', lineHeight: 21, color: th.text },
    lead: { fontSize: 15, lineHeight: 21.75, color: th.text },
    manage: { alignSelf: 'flex-start', minHeight: 28, justifyContent: 'center' },
    manageText: { fontSize: 14, fontWeight: '500', lineHeight: 20, color: th.accent, textDecorationLine: 'underline' },
    pressed: { opacity: 0.6 },
    text: { fontSize: 14, lineHeight: 20.3, color: th.textSub },
    caption: { fontSize: 13, lineHeight: 19.5, color: th.caption },
    error: { fontSize: 14, lineHeight: 20.3, color: th.danger },
  }),
);
