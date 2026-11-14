# E2E (Maestro, Android test phone or emulator)

**Since #57 (2026-11-13) these flows do not run.** Expo Go has no Tailscale module, so the app there cannot reach a PC, and pairing is now done by tapping the PC in the app (#58), not by pasting a link first. They wait for a rewrite against the built app (`app.askorus`). Until then see docs/testing.md, section 4 and MT-14, MT-15. What follows describes the flows as they were.

These flows drive the app in Expo Go on the test phone (USB debugging,
wireless as a fallback) or the emulator (`-Emulator`) against the real PC
agent and the real `claude`, in the test folder `rc-test` only. See `docs/testing.md`
section 4.

Run (PowerShell):

```
powershell -NoProfile -File scripts\e2e.ps1            # all flows
powershell -NoProfile -File scripts\e2e.ps1 -Flow 01   # one flow
```

Write a new flow by walking it on the test phone first with
`scripts\phone.ps1` (agent-device) and copying the texts its snapshot
shows. When a flow fails, `e2e.ps1` leaves `after-failure.txt` and
`after-failure.png` (the screen at that moment) in Maestro's report folder.
See `docs/testing.md` section 4.

The app on the device must be paired with the agent once (a person allows it on
the PC). Flows use the English UI (the phone is set to English).
