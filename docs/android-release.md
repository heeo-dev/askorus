# Android 배포 (B2)

2026-10-28 작성. B2 관문은 'Play 내부 테스트 트랙으로 받은 앱을 본인 폰에서 1주일 실제 사용'이다(`phase1-plan.md` 2절, 15절).

## 1. 설정

| 항목 | 값 | 어디 |
| --- | --- | --- |
| 패키지 이름 | `app.asksemble` (2026-10-28 사용자 확정. Play에 처음 올리면 바꿀 수 없다) | `app/app.json` `android.package` |
| Expo 프로젝트 | `@heeodev/asksemble`(개인 계정 owner) | `app/app.json` `owner`, `extra.eas.projectId` |
| 보이는 버전 | `version` (지금 0.1.0) | `app/app.json` |
| 빌드 번호(versionCode) | EAS가 보관하고 store 빌드마다 1씩 올린다 | `app/eas.json` `cli.appVersionSource: remote` |
| 시험용 빌드 | `preview`: APK, 링크·QR로 폰에 바로 설치 | `npm run build:apk` |
| 스토어 빌드 | `production`: AAB, Play 내부 테스트에 초안으로 올림 | `npm run build:aab`, `npm run eas -- submit -p android` |
| 서명 키 | EAS가 처음 빌드 때 만들고 보관한다. Play에는 업로드 키로 쓰이고, Play가 배포용 키로 다시 서명한다 | EAS 계정 |
| 권한 | 카메라(QR), 인터넷만. Expo가 기본으로 넣는 저장소·다른 앱 위에 그리기·진동 권한은 뺀다 | `app/app.json` `blockedPermissions` |

EAS CLI는 전역 설치하지 않고 `npx eas-cli@25.3.0`으로 쓴다(`package.json` scripts). 올릴 때는 scripts의 버전 세 곳을 같이 바꾼다.

Expo Go와 다른 점(스토어 빌드에서만 드러남, `src/config-test.ts`가 지킴):

- 다크 모드: `userInterfaceStyle: automatic`은 `expo-system-ui`가 있어야 적용된다. 없으면 늘 밝은 화면이다(R-14).
- 평문 HTTP: 릴리스 빌드는 막는다. 앱은 Tailscale serve의 https 주소만 쓰므로 바꿀 것이 없다.
- 딥 링크: `asksemble://pair`는 앱의 scheme으로 등록된다(Expo Go는 `exp://`).

## 2. 순서

| # | 누가 | 할 일 | 걸리는 시간 |
| --- | --- | --- | --- |
| 1 | 사용자 | PC에서 직접 연 PowerShell에서 `cd D:\ClaudeHome\Asksemble\app; npx eas-cli@25.3.0 login` (Expo 계정 비밀번호는 Claude에게 주지 않는다) | 1분 |
| 2 | Claude | `npm run eas -- init`: Expo에 프로젝트를 만들고 `app.json`에 projectId·owner를 적는다 | 1분 |
| 3 | Claude | `npm run build:apk`: 클라우드에서 APK 빌드. 처음이라 서명 키를 EAS에 만든다 | 10~20분(무료 요금제는 대기열) |
| 4 | 사용자 | 폰에서 빌드 링크를 열어 APK 설치('출처를 알 수 없는 앱' 허용), 짝 맺기부터 MT 목록 확인 | 10분 |
| 5 | 사용자 | Google Play Console 개발자 등록(개인, 25달러 1회), 신원 확인 | 며칠 걸릴 수 있음 |
| 6 | 사용자 | Play Console에서 앱 만들기(이름 Asksemble, 앱, 무료), 내부 테스트에 테스터(본인 Gmail) 추가 | 10분 |
| 7 | 사용자 | Google Cloud 서비스 계정 키를 만들어 EAS 대시보드에 올림(Claude는 키 파일을 받지 않는다) | 15분 |
| 8 | Claude | `npm run build:aab` → `npm run eas -- submit -p android`: 내부 테스트에 초안으로 올림 | 20분 |
| 9 | 사용자 | Play Console에서 초안을 출시, 폰에서 테스트 참여 링크로 설치 | 10분 |
| 10 | 사용자 | 1주일 실제 사용. 문제는 Claude에게 말하면 회귀 표에 적고 고친다 | 1주 |

3~4로 Play 계정이 열리기 전에 1주일 사용을 먼저 시작할 수 있다. 다만 Play가 다른 키로 다시 서명하므로, 9에서 Play 앱을 설치하려면 APK 앱을 먼저 지워야 하고 짝 맺기를 다시 한다.

## 3. Play Console에 쓸 것

내부 테스트는 심사가 없어 대부분 나중에 채워도 되지만, 비공개·프로덕션 테스트 전에는 모두 필요하다.

- **개인정보처리방침 URL**: 초안은 `docs/privacy.md`. 공개 주소가 필요하다(저장소를 공개하기 전이면 따로 올릴 곳을 정한다).
- **데이터 보안 양식**: 개발자가 받는 데이터 없음. 앱은 데이터를 수집·공유하지 않는다. 짝 맺은 PC 주소와 기기 토큰은 폰의 보안 저장소에만 두고, 통신은 사용자의 Tailscale 망 안에서 사용자의 PC와만 한다. 카메라는 QR 읽기에만 쓰고 사진을 저장하거나 보내지 않는다.
- **앱 이름·설명**: 이름은 Asksemble. 설명 끝에 '공식 Anthropic·Claude 제품이 아니다'를 적는다. 짧은 설명 예: 'Ask around, all at once' / '내 PC의 코딩 AI를 폰에서 켜고 이어 쓰기'.
- **아이콘**: 지금은 Expo 템플릿 아이콘이다. 스토어 등록 정보(512px)를 쓰기 전에 새로 만든다. Claude·Anthropic 로고나 이름은 쓰지 않는다.
- **콘텐츠 등급·대상 연령·광고 없음**: 설문으로 답한다.

알아 둘 것(2026-10-28 Google 도움말로 확인): 개인 계정은 만 18세 이상, 등록비 25달러 1회(선불카드 안 됨), 본인 이름의 신분증·카드를 물을 수 있고 잘못되면 등록비를 돌려주지 않는다. 계정 종류(개인·조직)는 나중에 못 바꾼다. 앱을 내기 전에 Play Console 모바일 앱으로 Android 기기 인증을 한다. 2023-11-13 뒤에 만든 개인 계정은 프로덕션 전에 비공개 테스트(테스터 12명이 14일 연속 참여)를 거치고 '프로덕션 신청'을 하며 심사는 보통 7일 안이다. 내부 테스트에는 이 조건이 없다. Phase 1 공개 전에 일정에 넣는다.
