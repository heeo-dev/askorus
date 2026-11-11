// The camera that reads the pairing QR code (#32). It is the app's own
// native view (modules/qr-scanner: CameraX shows the camera, ZXing reads the
// code), and the frames never leave the phone. The camera library used
// before read codes with Google ML Kit, which sends diagnostics and usage
// numbers to Google; the app promises to collect nothing, so it went.
//
// Android only for now. Where the native view is missing (Expo Go, iOS until
// its build, tests) scannerAvailable() is false and the pairing screen points
// to pasting the link instead.

import { requireNativeView, requireOptionalNativeModule } from 'expo';
import { type ComponentType, useCallback, useEffect, useState } from 'react';
import { AppState, PermissionsAndroid, Platform, type StyleProp, type ViewStyle } from 'react-native';

type NativeProps = {
  style?: StyleProp<ViewStyle>;
  onScanned: (event: { nativeEvent: { data: string } }) => void;
};

let view: ComponentType<NativeProps> | null | undefined;

function nativeView(): ComponentType<NativeProps> | null {
  if (view === undefined) {
    const there = Platform.OS === 'android' && requireOptionalNativeModule('AskorusQrScanner') !== null;
    view = there ? requireNativeView<NativeProps>('AskorusQrScanner') : null;
  }
  return view;
}

/** Whether this build can scan a QR code at all. */
export function scannerAvailable(): boolean {
  return nativeView() !== null;
}

/**
 * Shows the back camera and calls onScanned with the text of every QR code it
 * reads (the same code again about every 0.7 s while it stays in view). Put
 * it on screen only once the camera is allowed (useCameraPermission).
 */
export function QrScanner({ style, onScanned }: { style?: StyleProp<ViewStyle>; onScanned: (data: string) => void }) {
  const Native = nativeView();
  if (!Native) return null;
  return <Native style={style} onScanned={(e) => onScanned(e.nativeEvent.data)} />;
}

export type CameraPermission = { granted: boolean; canAskAgain: boolean };

/**
 * The camera permission, and a function that asks for it. null until the
 * first look. canAskAgain turns false once Android will not show its dialog
 * any more (the person refused twice, or chose 'Don't allow'): then only the
 * phone's Settings can allow it, and the answer is read again when the app
 * comes back to the front.
 */
export function useCameraPermission(): [CameraPermission | null, () => Promise<void>] {
  const [state, setState] = useState<CameraPermission | null>(null);

  const look = useCallback(async () => {
    const granted =
      Platform.OS === 'android' && (await PermissionsAndroid.check(PermissionsAndroid.PERMISSIONS.CAMERA).catch(() => false));
    setState((old) => ({ granted, canAskAgain: granted || (old?.canAskAgain ?? true) }));
  }, []);

  useEffect(() => {
    void look();
    const sub = AppState.addEventListener('change', (now) => {
      if (now === 'active') void look();
    });
    return () => sub.remove();
  }, [look]);

  const ask = useCallback(async () => {
    if (Platform.OS !== 'android') return;
    const answer = await PermissionsAndroid.request(PermissionsAndroid.PERMISSIONS.CAMERA).catch(() => PermissionsAndroid.RESULTS.DENIED);
    setState({
      granted: answer === PermissionsAndroid.RESULTS.GRANTED,
      canAskAgain: answer !== PermissionsAndroid.RESULTS.NEVER_ASK_AGAIN,
    });
  }, []);

  return [state, ask];
}
