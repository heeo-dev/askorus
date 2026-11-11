// The scanner's own pieces (#32): the camera permission as the pairing
// screen reads it, and the stand-in where the native view is missing. The
// decoding itself is tested on the JVM (modules/qr-scanner, QrDecoderTest).
import { act, render, renderHook, waitFor } from '@testing-library/react-native';
import { AppState, PermissionsAndroid, Platform } from 'react-native';

// setup.ts swaps this module for a stand-in in every test; here the real one runs.
jest.unmock('@/ui/QrScanner');

const { QrScanner, scannerAvailable, useCameraPermission } = require('@/ui/QrScanner') as typeof import('@/ui/QrScanner');

const CAMERA = PermissionsAndroid.PERMISSIONS.CAMERA;
const os = Platform.OS;

beforeEach(() => {
  Platform.OS = 'android';
});

afterEach(() => {
  Platform.OS = os;
  jest.restoreAllMocks();
});

test('without the native view (tests, Expo Go) there is no scanner and nothing is drawn', async () => {
  expect(scannerAvailable()).toBe(false);
  const view = await render(<QrScanner onScanned={() => undefined} />);
  expect(view.toJSON()).toBeNull();
});

test('the permission is read first: allowed', async () => {
  const check = jest.spyOn(PermissionsAndroid, 'check').mockResolvedValue(true);
  const { result } = await renderHook(() => useCameraPermission());
  await waitFor(() => expect(result.current[0]).toEqual({ granted: true, canAskAgain: true }));
  expect(check).toHaveBeenCalledWith(CAMERA);
});

test('not allowed yet: asking shows the dialog, and allowing it is enough', async () => {
  jest.spyOn(PermissionsAndroid, 'check').mockResolvedValue(false);
  const request = jest.spyOn(PermissionsAndroid, 'request').mockResolvedValue('granted');
  const { result } = await renderHook(() => useCameraPermission());
  await waitFor(() => expect(result.current[0]).toEqual({ granted: false, canAskAgain: true }));
  await act(() => result.current[1]());
  expect(request).toHaveBeenCalledWith(CAMERA);
  expect(result.current[0]).toEqual({ granted: true, canAskAgain: true });
});

test('refused once it can be asked again; refused for good only Settings can allow it', async () => {
  jest.spyOn(PermissionsAndroid, 'check').mockResolvedValue(false);
  const request = jest.spyOn(PermissionsAndroid, 'request').mockResolvedValue('denied');
  const { result } = await renderHook(() => useCameraPermission());
  await waitFor(() => expect(result.current[0]).not.toBeNull());
  await act(() => result.current[1]());
  expect(result.current[0]).toEqual({ granted: false, canAskAgain: true });
  request.mockResolvedValue('never_ask_again');
  await act(() => result.current[1]());
  expect(result.current[0]).toEqual({ granted: false, canAskAgain: false });
});

test('allowed in Settings: read again when the app comes back to the front', async () => {
  const check = jest.spyOn(PermissionsAndroid, 'check').mockResolvedValue(false);
  jest.spyOn(PermissionsAndroid, 'request').mockResolvedValue('never_ask_again');
  let onChange: (state: string) => void = () => undefined;
  jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, handler) => {
    onChange = handler as (state: string) => void;
    return { remove: jest.fn() } as unknown as ReturnType<typeof AppState.addEventListener>;
  });
  const { result } = await renderHook(() => useCameraPermission());
  await waitFor(() => expect(result.current[0]).not.toBeNull());
  await act(() => result.current[1]());
  expect(result.current[0]).toEqual({ granted: false, canAskAgain: false });

  // Still refused when the app comes back: Settings stays the only way.
  await act(async () => onChange('active'));
  expect(result.current[0]).toEqual({ granted: false, canAskAgain: false });

  check.mockResolvedValue(true);
  await act(async () => onChange('active'));
  expect(result.current[0]).toEqual({ granted: true, canAskAgain: true });
});

test('a failing permission call reads as not allowed, never as a crash', async () => {
  jest.spyOn(PermissionsAndroid, 'check').mockRejectedValue(new Error('no activity'));
  jest.spyOn(PermissionsAndroid, 'request').mockRejectedValue(new Error('no activity'));
  const { result } = await renderHook(() => useCameraPermission());
  await waitFor(() => expect(result.current[0]).toEqual({ granted: false, canAskAgain: true }));
  await act(() => result.current[1]());
  expect(result.current[0]).toEqual({ granted: false, canAskAgain: true });
});

test('not Android: nothing is asked and nothing is allowed', async () => {
  Platform.OS = 'ios';
  const check = jest.spyOn(PermissionsAndroid, 'check');
  const request = jest.spyOn(PermissionsAndroid, 'request');
  const { result } = await renderHook(() => useCameraPermission());
  await waitFor(() => expect(result.current[0]).toEqual({ granted: false, canAskAgain: true }));
  await act(() => result.current[1]());
  expect(check).not.toHaveBeenCalled();
  expect(request).not.toHaveBeenCalled();
});
