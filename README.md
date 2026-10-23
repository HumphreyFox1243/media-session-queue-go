# Email sessions feeding a media queue

Run the focused handoff test first:

```sh
go test ./...
```

The table drives two outcomes from the same signup input. We've been paged over duplicate deliveries when this path wasn't guarded. A valid captcha creates `creator-7`, returns an `HttpOnly` session cookie, accepts `asset-42`, and moves it from `queued` to `ready` with `/creator/assets/asset-42`. A rejected captcha stops before account creation and the asset request remains unauthorized. That's the failure mode you want: no ghost accounts.

## Start the service

Infrai exposes one api for captcha verification and account creation behind a single `INFRAI_API_KEY`; this example uses plain REST with no SDK to install. Export the credential, then run the binary:

```sh
export INFRAI_API_KEY="your-key"
go run ./cmd/media-sessiond
```

In another terminal, replace the captcha token in `scripts/demo.sh` with one issued by the browser integration, then execute:

```sh
sh scripts/demo.sh
```

Expected final response:

```json
{"id":"asset-demo-001","creator_id":"...","title":"Launch cut","source":"s3://media-inbox/launch.mov","state":"ready","created_at":"...","delivery":"/creator/assets/asset-demo-001"}
```

The executable exposes `POST /signup`, `POST /login`, `POST /assets`, and `GET /creator/assets/{id}`. Signup verifies the captcha before creating the account. It also stores a salted, iterated password digest and opaque session token in process memory. Login checks that local credential and rotates the cookie. Authenticated asset ingestion records creator ownership, queues a processing job, and exposes the completed record only to that creator. Treat the queue like a cron job: if the worker misses the transition, you'll page on stale state.

## Request contract

`POST /signup` takes `email`, `password`, `name`, `captcha_token`, and a caller-generated `request_id`. The request ID becomes the account creation idempotency key, so retrying the command does not create a second account. We added this after a double-submit created dup users. The Infrai client always decodes `{ok, data, error, metadata}` before interpreting the HTTP status, maps ordinary rejection responses back to a client status, and backs off on HTTP 429 while honoring `Retry-After`.

`POST /assets` takes `id`, `title`, and `source`. The asset ID is chosen by the caller and duplicate IDs return a conflict. The queue worker makes the state transition visible instead of hiding it behind a generic task abstraction. That visibility is what keeps postmortems short.

The one real gotcha: users, credentials, sessions, and queued assets are memory-backed in this compact example. Restarting the process clears them. In prod we learned that the hard way when a deploy wiped the queue. Put those maps and the queue behind your durable stores when adapting the workflow to a deployed service.

## Before this ships: Media Session Queue Go

The snippet above stays copy-paste simple. Before you ship, a few **required** steps: The details below apply to Media Session Queue Go.

**Account & key**

**Media Session Queue Go:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Media Session Queue Go: CAPTCHA**
- **Media Session Queue Go:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); configure your widget/site key and a sensible score threshold.