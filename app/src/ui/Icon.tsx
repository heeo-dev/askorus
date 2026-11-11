// The app's line icons (S5c), drawn from the design boards' SVG into white
// PNGs (scripts/render-icons.mjs) and tinted here, so no SVG library is
// needed and it runs in Expo Go. An icon is decoration: the control around
// it carries the words.

import { Image, type ImageSourcePropType, type ImageStyle, Pressable, type StyleProp, type ViewStyle } from 'react-native';

import { useTheme } from './theme';

export type IconName =
  | 'menu'
  | 'compose'
  | 'ask'
  | 'pc'
  | 'history'
  | 'chevronRight'
  | 'chevronDown'
  | 'chevronUp'
  | 'back'
  | 'send'
  | 'info'
  | 'close'
  | 'check'
  | 'copy'
  | 'terminal'
  | 'open'
  | 'search'
  | 'retry'
  | 'plus'
  | 'paste'
  | 'stop'
  | 'phone'
  | 'switch'
  | 'trash'
  | 'folder'
  | 'camera'
  | 'shield'
  | 'alert'
  | 'sliders'
  | 'settings'
  | 'pending'
  | 'more'
  | 'share'
  | 'flag';

const sources: Record<IconName, ImageSourcePropType> = {
  menu: require('../../assets/icons/menu.png'),
  compose: require('../../assets/icons/compose.png'),
  ask: require('../../assets/icons/ask.png'),
  pc: require('../../assets/icons/pc.png'),
  history: require('../../assets/icons/history.png'),
  chevronRight: require('../../assets/icons/chevronRight.png'),
  chevronDown: require('../../assets/icons/chevronDown.png'),
  chevronUp: require('../../assets/icons/chevronUp.png'),
  back: require('../../assets/icons/back.png'),
  send: require('../../assets/icons/send.png'),
  info: require('../../assets/icons/info.png'),
  close: require('../../assets/icons/close.png'),
  check: require('../../assets/icons/check.png'),
  copy: require('../../assets/icons/copy.png'),
  terminal: require('../../assets/icons/terminal.png'),
  open: require('../../assets/icons/open.png'),
  search: require('../../assets/icons/search.png'),
  retry: require('../../assets/icons/retry.png'),
  plus: require('../../assets/icons/plus.png'),
  paste: require('../../assets/icons/paste.png'),
  stop: require('../../assets/icons/stop.png'),
  phone: require('../../assets/icons/phone.png'),
  switch: require('../../assets/icons/switch.png'),
  trash: require('../../assets/icons/trash.png'),
  folder: require('../../assets/icons/folder.png'),
  camera: require('../../assets/icons/camera.png'),
  shield: require('../../assets/icons/shield.png'),
  alert: require('../../assets/icons/alert.png'),
  sliders: require('../../assets/icons/sliders.png'),
  settings: require('../../assets/icons/settings.png'),
  pending: require('../../assets/icons/pending.png'),
  more: require('../../assets/icons/more.png'),
  share: require('../../assets/icons/share.png'),
  flag: require('../../assets/icons/flag.png'),
};

export const iconNames = Object.keys(sources) as readonly IconName[];

export function Icon({ name, size = 24, color, style }: { name: IconName; size?: number; color: string; style?: StyleProp<ImageStyle> }) {
  return (
    <Image
      source={sources[name]}
      style={[{ width: size, height: size, tintColor: color }, style]}
      resizeMode="contain"
      accessible={false}
      importantForAccessibility="no"
      testID={`icon-${name}`}
    />
  );
}

/** IconButton is a 44×44 button showing only an icon; label is what a screen reader says. */
export function IconButton({
  name,
  label,
  onPress,
  size = 22,
  color,
  disabled,
  hint,
  style,
}: {
  name: IconName;
  label: string;
  onPress: () => void;
  size?: number;
  color?: string;
  disabled?: boolean;
  hint?: string;
  style?: StyleProp<ViewStyle>;
}) {
  const th = useTheme();
  return (
    <Pressable
      onPress={onPress}
      disabled={disabled}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityHint={hint}
      accessibilityState={{ disabled: !!disabled }}
      style={({ pressed }) => [
        { width: 44, height: 44, alignItems: 'center', justifyContent: 'center', borderRadius: 22 },
        pressed && { backgroundColor: th.raised },
        disabled && { opacity: 0.4 },
        style,
      ]}
    >
      <Icon name={name} size={size} color={color ?? th.textSub} />
    </Pressable>
  );
}
