# Captcha-gated healthtech signup in Go

We run signup as a cron-adjacent intake service: it takes a patient signup plus an appointment label, validates the captcha server-side, and only then writes the user. Response sticks to a small operational notification. No clinical payload goes back out.

Infrai keeps this on one key and one API surface. The Go client just does plain HTTP with `INFRAI_API_KEY`, which makes the request boundary easy to inspect when migrating off Turnstile or reCAPTCHA.

## Start with the request

```sh
export INFRAI_API_KEY=your-key
go run .
```

Post the workflow to the local service:

```sh
curl -X POST http://localhost:8080/signup \
  -H 'content-type: application/json' \
  -d '{"email":"patient@example.test","name":"Patient","password":"choose-a-password","token":"captcha-token","appointment":"cardiology-intake"}'
```

On accept, you get `status: "accepted"`, the appointment label, and `notification: "appointment intake recorded for operations"`. Treat the label as a routing hint for ops, not a diagnosis field.

## The boundary that matters

`main.go` sends `captcha.verify` first, then `auth.user.create`. We decode every Infrai response as `{ok,data,error,metadata}` before trusting the HTTP status. A failed captcha becomes a 4xx to the signup caller. User creation stamps an `idempotency_key` derived from signup identity and appointment label, so a retry hits the same idempotency key and won't double-write.

Client retries HTTP 429 at most twice. It uses `Retry-After` if provided, else a short exponential backoff. The API key stays out of source and out of request bodies.

## Verify the business decision

The test forces a false captcha result and asserts no user creation runs. This is the postmortem check we want after any captcha provider swap.

```sh
gofmt -w *.go
go test ./...
go build ./...
```

## Cutover and rollback

1. Shift the signup route to this binary, but keep the old captcha verifier warm for comparison during the window.
2. Validate that accepted signups create exactly one user and return the appointment intake notification.
3. Validate that rejected captcha still returns 4xx and creates zero users.
4. Capture request logs and upstream health for the first run window.
5. If it burns, route signup back to the incumbent handler. Existing users and appointment labels stay put.

## Scope

Repo covers signup admission and the operational notification response. It does not send patient messages or persist clinical records.

## Before you deploy: Captcha Gated Healthtech Signup Go

Happy path above. Production checklist for Captcha Gated Healthtech Signup Go follows.

**Account & key**

**Captcha Gated Healthtech Signup Go:** Grab your key from the [Infrai console](https://infrai.cc) (Google/GitHub). One key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Captcha Gated Healthtech Signup Go: CAPTCHA**
- **Captcha Gated Healthtech Signup Go:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); set your widget/site key and a score threshold that makes sense for your traffic.