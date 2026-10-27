# E2E (Maestro, Android emulator)

These flows drive the app in Expo Go on the emulator against the real PC
agent and the real `claude`, in the test folder `rc-test` only. See
`docs/testing.md` section 4.

Run (PowerShell):

```
powershell -NoProfile -File scripts\e2e.ps1            # all flows
powershell -NoProfile -File scripts\e2e.ps1 -Flow 01   # one flow
```

The emulator app must be paired with the agent once (a person allows it on
the PC). Flows use the English UI (the emulator's language).
