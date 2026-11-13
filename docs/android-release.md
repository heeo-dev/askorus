# Android 배포 (B2)

2026-10-28 작성, 2026-11-10 고침. B2 관문은 'Play 내부 테스트 트랙으로 받은 앱을 본인 폰에서 1주일 실제 사용'이다(`phase1-plan.md` 2절, 15절). 이 문서는 빌드 설정과 출시 규칙만 다룬다. 작성자 계정으로 하는 등록과 순서는 작성자의 로컬 메모(`.local/play-console.md`, git에 올리지 않음)에 있다.

## 1. 설정

| 항목 | 값 | 어디 |
| --- | --- | --- |
| 패키지 이름 | `app.askorus` (2026-11-08 이름을 Askorus로 바꾸며 `app.asksemble`에서 바꿈, #31. Play에 처음 올리면 바꿀 수 없다) | `app/app.json` `android.package` |
| Expo 프로젝트 | `@heeodev/askorus`(개인 계정 owner, 2026-11-08 `eas init`으로 만듦, #31). 옛 `@heeodev/asksemble`은 Expo에 그대로 남아 있다. 서명 키는 로컬 빌드와 같은 것(옛 프로젝트에서 EAS가 만든 키, SHA-256 7E:98…)을 같은 날 새 프로젝트의 app.askorus에 올렸다(`.local\eas-upload-key.cmd`, 사용자가 실행). 그래서 클라우드 빌드도 폰의 로컬 빌드 위에 덮어 깔린다 | `app/app.json` `owner`, `extra.eas.projectId` |
| 보이는 버전 | `version` (지금 0.3.0, 2026-11-13) | `app/app.json` |
| 빌드 번호(versionCode) | EAS가 보관하고 store 빌드마다 1씩 올린다. 로컬 빌드는 빌드할 때 번호를 준다(안 주면 1). Play는 앞에 올린 것보다 큰 번호만 받으므로, 로컬에서 만든 AAB를 올린 뒤 EAS로 넘어갈 때는 EAS의 번호를 그 위로 맞춘다. 2026-11-11 기준 로컬에서 만든 AAB는 2까지다 | `app/eas.json` `cli.appVersionSource: remote` |
| 시험용 빌드 | `preview`: APK, 링크·QR로 폰에 바로 설치 | `npm run build:apk` |
| 스토어 빌드 | `production`: AAB, Play 내부 테스트에 초안으로 올림 | `npm run build:aab`, `npm run eas -- submit -p android` |
| 서명 키 | EAS가 처음 빌드 때 만들고 보관한다. Play에는 업로드 키로 쓰이고, Play가 배포용 키로 다시 서명한다 | EAS 계정 |
| 권한 | 카메라(QR), 인터넷, 네트워크 상태(CameraX가 끌어오는 media3의 일반 권한)뿐이다(네트워크 상태는 앱의 Tailscale 모듈도 쓴다: 폰의 네트워크가 바뀐 것을 내장 기기에 알린다, #57. VPN 권한은 없다). Expo가 기본으로 넣는 저장소·다른 앱 위에 그리기·진동 권한과, 보안 저장소 라이브러리가 넣는 생체 인증 권한 두 개(`USE_BIOMETRIC`, `USE_FINGERPRINT`)는 뺀다(2026-11-11, #32: 앱은 생체 인증으로 잠그는 저장을 쓰지 않는다. 쓰게 되면 먼저 다시 넣는다). 빌드한 파일의 권한은 `aapt2 dump badging <apk>`로 확인한다 | `app/app.json` `blockedPermissions` |

EAS CLI는 전역 설치하지 않고 `npx eas-cli@25.3.0`으로 쓴다(`package.json` scripts). 올릴 때는 scripts의 버전 세 곳을 같이 바꾼다.

Expo Go와 다른 점(스토어 빌드에서만 드러남, `src/config-test.ts`가 지킴):

- 다크 모드: `userInterfaceStyle: automatic`은 `expo-system-ui`가 있어야 적용된다. 없으면 늘 밝은 화면이다(R-14).
- 평문 HTTP: 릴리스 빌드는 막는다(Android의 HTTP 스택과 WebView). 앱이 PC로 보내는 요청은 Android의 HTTP 스택이 아니라 앱에 내장한 Tailscale 기기(Go 라이브러리)가 망 안에서 보내고, 망이 두 기기 사이를 암호화한다(#57). 그래서 `usesCleartextTraffic`을 켜지 않으며 바꿀 것이 없다.
- Tailscale: 앱에 내장된다(`app/modules/tailnet`, 아래 1.1). Expo Go에는 이 모듈이 없어 PC에 연결할 수 없다. 화면만 볼 때 말고는 빌드한 앱으로 확인한다.
- 딥 링크: `askorus://pair`는 앱의 scheme으로 등록된다(Expo Go는 `exp://`).

### 1.1 Tailscale 다리 빌드 (#57)

앱의 Tailscale 부분은 Go 라이브러리(`tsbridge/`, Tailscale의 `tsnet`)다. gomobile로 Android 라이브러리를 만들어 앱의 모듈에 넣는다. 만든 파일은 git에 넣지 않으므로, 앱을 빌드하기 전에 만들고 `tsbridge/`나 Tailscale 판이 바뀌면 다시 만든다.

```sh
go install golang.org/x/mobile/cmd/gomobile@latest golang.org/x/mobile/cmd/gobind@latest   # 처음 한 번
export ANDROID_HOME=<Android SDK> ANDROID_NDK_HOME=<그 안의 NDK> JAVA_HOME=<JDK 17>
tsbridge/build-android.sh arm64                 # 시험용 APK (arm64만)
tsbridge/build-android.sh arm64,arm,amd64,386   # 스토어에 올릴 AAB (네 가지 모두)
```

- 스크립트가 빌드 태그 `ts_omit_peerapiserver,ts_omit_c2n,ts_omit_logtail`를 붙인다(Tailscale 라이브러리의 다른 기기용 서비스, 서버 진단, 로그 올리기를 뺀다. `docs/phase1-plan.md` 25절 13). 태그 없이 만든 라이브러리는 앱에서 켜지지 않는다.
- 나오는 것: `app/modules/tailnet/android/libs/tsbridge.jar`와 `src/main/jniLibs/<abi>/libgojni.so`. 없으면 Gradle이 `checkTsbridge`에서 멈추고 이 명령을 알려 준다.
- 빌드한 ABI와 앱 빌드의 ABI(`-PreactNativeArchitectures`)가 같아야 한다. 라이브러리가 없는 ABI의 폰에서는 앱이 PC에 연결하지 못한다.
- 크기: arm64 하나가 약 21MB다. APK에는 압축하지 않고 담기므로 APK 파일이 그만큼 커진다(11-13 시험 APK: 37MB에서 52MB). 스토어에서 내려받는 크기로는 약 8MB 늘어난다. 16KB 메모리 페이지에 맞춰 링크한다(Android 15, Play 요구).
- EAS 클라우드 빌드에는 Go와 gomobile이 없다. EAS로 빌드하려면 빌드 전 단계(`eas-build-pre-install`)에서 위 명령을 돌리게 해야 한다(아직 안 함. 지금은 로컬 빌드만).
- Tailscale 판을 올릴 때: `tsbridge`와 `agent`의 `go.mod`를 같이 올리고, S10에서 로그인, 짝 맺기, 묻기를 다시 확인한다(Android의 인터페이스 목록과 로그 폴더 대비 코드가 Tailscale 안쪽에 기대고 있다, `tsbridge/madeup.go`).

## 2. Play에 올리기

- **첫 업로드는 직접**: Google Play API가 새 앱의 첫 업로드를 받지 않아서, 첫 AAB는 Play Console 웹에서 올린다(앱 서명은 기본값, Google이 만든 키). 두 번째부터 EAS submit으로 올리려면 Google Cloud 서비스 계정 키를 EAS에 올려 둔다([Expo 문서](https://docs.expo.dev/submit/android/)).
- **오픈소스 라이선스 목록**(#32): 앱의 설정 › 정보 › 오픈소스 라이선스가 보여 주는 목록은 `app/src/licenses/licenses.json`이다. 패키지를 더하거나 올리면 `app`에서 `npm run licenses`로 다시 만든다(안 하면 `licenses-test`가 실패한다). 내장 Tailscale의 Go 모듈도 이 목록에 들어간다(#57): `agent/go.mod`나 `tsbridge/go.mod`가 바뀌면 `npm run go-libs`(Go가 PATH에 있어야 함)를 먼저 돌린다. 이것이 `app/scripts/go-libs.json`과 저장소의 `THIRD-PARTY-GO.md`(에이전트와 앱의 Go 모듈과 라이선스 글 전부)를 다시 쓴다(안 하면 `go-libs-test`가 실패한다). Expo나 React Native를 올리면 안드로이드 라이브러리도 다시 뽑는다: `app/android`에서 `gradlew :app:dependencies --configuration releaseRuntimeClasspath --console=plain > deps.txt` 뒤 `node ../scripts/android-libs.mjs deps.txt <GRADLE_USER_HOME>`, 그리고 `npm run licenses`. APK나 AAB를 배포할 때 이 목록이 앱 안의 저작권 표시다. 저장소의 표시는 `THIRD-PARTY-NOTICES.md`.
- **GitHub 배포판의 미리보기 APK**(11-11~, Play에 나오기 전까지): 배포판마다 main의 커밋에서 빌드한 APK를 `askorus-android-<판>.apk`로 붙이고(`gh release upload`), `THIRD-PARTY-NOTICES.md`와 `THIRD-PARTY-GO.md`도 같이 붙인다. 올리기 전에 APK 안에 개인 값이 없는지 훑고, 배포 글에 APK의 SHA-256과 빌드한 커밋을 적는다. README의 '안드로이드 앱 설치'가 최신 배포판을 가리키므로 새 배포판에 APK가 빠지면 안 된다. 알릴 것 세 가지는 README에 있다(자동 업데이트 없음, Play 앱으로 옮길 때 다시 설치, 파일 확인).
- **빌드 번호**: Play는 이미 올린 번호보다 큰 번호만 받는다. 로컬 빌드로 첫 AAB를 올렸으면 `npm run eas -- build:version:set -p android`로 EAS가 보관하는 번호를 Play에 올린 번호 이상으로 맞춘다.
- **다시 서명**: Play가 배포용 키로 다시 서명하므로, APK로 깐 앱 위에 Play 앱을 덮어 깔 수 없다. APK 앱을 지우고(폰의 기록도 지워짐) 짝 맺기를 다시 한다.
- **트랙**: 내부 테스트는 심사와 조건이 없다. 2023-11-13 뒤에 만든 개인 개발자 계정은 프로덕션 전에 비공개 테스트(테스터 12명이 14일 연속 참여)를 거쳐 '프로덕션 신청'을 하고, 심사는 보통 7일 안이다([Google 도움말](https://support.google.com/googleplay/android-developer/answer/14151465)).
- **무료**: 무료로 낸 앱은 나중에 유료로 바꿀 수 없다.

## 3. 스토어에 쓸 것

내부 테스트는 심사가 없어 대부분 나중에 채워도 되지만, 비공개·프로덕션 테스트 전에는 모두 필요하다.

- **개인정보처리방침 URL**: 글의 기준은 `docs/privacy.md`, 웹페이지 원본은 `docs/privacy/index.html`. 공개 주소는 GitHub Pages의 `https://heeo-dev.github.io/askorus/privacy/`다(#40, main의 `/docs`). 시행일은 2026-11-11이고, 문의 연락처는 Play에 공개하는 개발자 이메일과 같은 주소를 쓴다(2026-11-11 적음). 글을 고치면 두 파일을 같이 고치고 시행일도 바꾼다.
- **데이터 보안 양식**: 개발자가 받는 데이터 없음. **#57로 다시 답해야 한다**: 앱에 내장한 Tailscale이 사용자가 로그인한 뒤 Tailscale의 서버에 기기 이름과 IP 주소를 보낸다(기기 식별 정보, 앱 기능에 필요, 사용자가 직접 시작한 로그인). Play의 정의로는 앱에 든 라이브러리가 보내는 것도 앱이 보내는 것이다. 아래는 그 전의 답이다. 앱은 데이터를 수집·공유하지 않는다. 이 답이 사실이려면 앱에 들어가는 라이브러리도 아무것도 보내지 않아야 한다: QR은 앱의 모듈(`app/modules/qr-scanner`, CameraX + ZXing)이 폰 안에서 읽고, Google ML Kit·Play 서비스·Firebase처럼 진단이나 사용 통계를 보내는 라이브러리는 넣지 않는다(2026-11-11, #32, R-97. `licenses-test`가 릴리스 빌드의 라이브러리 목록에서 지킨다). 패키지를 더할 때는 `app/android`에서 의존성 목록을 다시 뽑아(2절의 라이선스 목록과 같은 명령) 새로 들어온 라이브러리를 본다. 짝 맺은 PC 주소와 기기 토큰은 폰의 보안 저장소에만 두고, 통신은 사용자의 Tailscale 망 안에서 사용자의 PC와만 한다. 카메라는 QR 읽기에만 쓰고 사진을 저장하거나 보내지 않는다.
- **앱 이름·설명**: 칸마다 넣을 글은 `docs/store/listing-ko.md`·`listing-en.md`. 설명 끝에 '공식 Anthropic·Claude 제품이 아니다'를 적는다.
- **그림**: 아이콘·대표 그림·스크린숏은 `docs/store/README.md`의 체크 목록. Claude·Anthropic 로고나 이름은 쓰지 않는다.
- **콘텐츠 등급·대상 연령·광고 없음**: 설문으로 답한다.
- **앱 액세스(심사자가 앱을 보는 방법)**: 앱 자체의 계정은 없다. PC에 연결하려면 Tailscale 계정으로 로그인한다(#57, 앱이 브라우저의 Tailscale 화면을 연다). 앱은 사용자 자신의 PC가 있어야 동작하므로, PC가 없는 심사자는 첫 화면의 '예시 보기'(See an example, #51)로 질문, 세 AI의 답, 종합 답변, 토론과 답 아래의 공유·신고(#50)를 본다. 안내 칸에는 이 길을 영어로 적는다.
