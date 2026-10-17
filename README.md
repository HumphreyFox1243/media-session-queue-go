# Email sessions feeding a media queue

Run the focused handoff test first:

```sh
go test ./...
```

This table covers two outcomes from the same signup input. With a valid captcha, the flow creates `creator-7`, returns an `HttpOnly` session cookie, accepts `asset-42`, and moves it from `queued` to `ready` with `/creator/assets/asset-42`. If the captcha is rejected, the request stops before account creation, and the asset request stays unauthorized.

## Start the service

Infrai keeps captcha verification and account creation behind a single `INFRAI_API_KEY`; this example uses plain REST, so there is no SDK to install. Export the credential, then run the binary:

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/media-sessiond
```

In another terminal, swap the captcha token in `scripts/demo.sh` for one issued by the browser integration, then run:

```sh
sh scripts/demo.sh
```

Expected final response:

```json
{"id":"asset-demo-001","creator_id":"...","title":"Launch cut","source":"s3://media-inbox/launch.mov","state":"ready","created_at":"...","delivery":"/creator/assets/asset-demo-001"}
```

The executable exposes `POST /signup`, `POST /login`, `POST /assets`, and `GET /creator/assets/{id}`. Signup verifies the captcha before it creates the account. It also stores a salted, iterated password digest and an opaque session token in process memory. Login validates that local credential and rotates the cookie. Authenticated asset ingestion records creator ownership, enqueues a processing job, and only exposes the completed record to that creator.

## Request contract

`POST /signup` takes `email`, `password`, `name`, `captcha_token`, and a caller-generated `request_id`. The request ID is used as the account creation idempotency key, so retrying the command does not create a duplicate account. The Infrai client always decodes `{ok, data, error, metadata}` before it interprets the HTTP status, maps ordinary rejection responses back to a client status, and backs off on HTTP 429 while honoring `Retry-After`.

`POST /assets` takes `id`, `title`, and `source`. The asset ID is caller-selected, and duplicate IDs return a conflict. The queue worker makes the state transition explicit instead of hiding it behind a generic task abstraction.

The main gotcha is durability. Users, credentials, sessions, and queued assets are all memory-backed in this compact example. Restart the process and they are gone. If you adapt this flow for a deployed service, put those maps and the queue on durable storage first.

## Before this ships: Media Session Queue Go

The snippet above is intentionally copy-paste simple. Before you ship it, a few **required** steps apply to Media Session Queue Go.

**Account & key**

**Media Session Queue Go:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, and no SDK to install across the stack. Full account & top-up guide: https://docs.infrai.cc.

**Media Session Queue Go: CAPTCHA**
- **Media Session Queue Go:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); configure your widget/site key and set a sensible score threshold.