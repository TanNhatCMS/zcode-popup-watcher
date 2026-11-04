# zcode-popup-watcher

Tools that watch for ZCode deals and notify you on screen, so you can
claim them yourself inside the official ZCode app.

**These tools never claim a deal for you.** They do not call the
marketing action endpoint, they do not click inside ZCode, and they do
not send your token anywhere except as an `Authorization` header to
`zcode.z.ai` when checking the marketing snapshot.

## What is watched

ZCode's in-app banner/popup system ("marketing-touch") works by
polling, not by push:

```
GET https://zcode.z.ai/api/v1/marketing/touch?seq=...
```

with `X-Device-Mid`, `X-Client-Language`, `X-ZCode-App-Version` and,
when logged in, `Authorization: Bearer <ZCode JWT>`. The server
returns a snapshot with `deliveries` (one `banner` and/or one
`popup`). The client only renders what the server returns, so the
campaign window, eligibility and remaining quota are all decided
server-side.

## Tools

TypeScript + Bun only. No dependencies, no install step. Requires [Bun](https://bun.sh).

| File | What it does |
| --- | --- |
| `bun/deal-checker.ts` | Polls `marketing/touch` every 600s (the production client's interval), shows a native notification when a new delivery appears. |
| `bun/popup-watcher.ts` | Time-window reminder: notifies once a day in the 17:55–22:10 VN window for the nightly deal. No API calls. |
| `bun/zcode-config.ts` | Reads `~/.zcode/v2` config (see below). |
| `bun/notify.ts` | Native notifications: macOS `osascript`, Linux `notify-send`, Windows MessageBox, console fallback. |

```sh
bun run bun/deal-checker.ts --once
bun run bun/deal-checker.ts            # background polling
bun run bun/deal-checker.ts --self-test
bun run bun/deal-checker.ts --dry-run  # config only, no API call
bun run bun/popup-watcher.ts --test
```

## Reading config from `~/.zcode/v2`

The checker reads, on the same machine and user as ZCode:

- `telemetry-state.json` → `deviceMid` (a plaintext UUID, not a secret).
- `credentials.json` → key `zcodejwttoken`, whose value is encrypted
  as `enc:v1:...` and decrypted with AES-256-GCM. The key is
  `SHA256(secret)`, where secret is `$ZCODE_CREDENTIAL_SECRET` if set,
  otherwise `zcode-credential-fallback:<platform>:<homedir>:<username>`
  — exactly ZCode's own scheme. Consequences:
  - The tool only works on the same machine/user as ZCode. A different
    machine, user, or secret fails closed instead of guessing a token.
  - If no custom secret is set, that fallback secret is not strong
    security: anyone running as the same user on the same machine can
    decrypt it, just like ZCode itself can.
  - The token is never printed, never written to a file or log, and
    only lives in RAM for the run. `--dry-run` prints only whether a
    token exists and a masked `deviceMid`.

A token supplied via `ZCODE_JWT_TOKEN` overrides `credentials.json`.

## Polling discipline

- Default interval is 600 seconds, matching ZCode's production
  poller. Do not go below 300 seconds: you risk HTTP 429, especially
  while the ZCode app itself is also polling.
- Failures back off exponentially up to 10 minutes, mirroring the
  app's poller.
- Seen deliveries are recorded in `~/.zcode_popup_watcher/seen_deals.json`
  (campaign keys only, never tokens) so a deal is notified once.

## Security notes

- Never paste your ZCode JWT into a chat, an issue, or a commit.
- These tools are read-only notifiers. If you want a deal, open ZCode
  and click the banner/popup inside the official app.
- Automated claiming of free quotas breaches Z.ai's terms and risks
  rate-limiting, revocation, or account suspension. That is
  deliberately not implemented here.
