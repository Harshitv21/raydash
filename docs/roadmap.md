# Raydash — 1-week build roadmap

A self-hosted, in-memory KV cache written in Go, with a custom TCP protocol (not RESP), a Java client SDK, and a Docker-based demo showing the cache-aside pattern.

## Architectural decisions (locked before Day 1)

- **Not a Redis protocol clone.** Implementing RESP properly (bulk strings, arrays, binary safety, pipelining) is a multi-day project on its own and teaches you someone else's spec, not protocol design. Skip it.
- **Not a wrapper.** You're writing the store, the TTL logic, and the server yourself.
- **Standalone cache, custom protocol, raw TCP.** You design the wire format, parse it yourself, and write both the Go server and a small Java client. This is where most of the actual learning lives.
- **Scope discipline is the real risk, not Go.** The single biggest way this week fails is chasing "just one more feature." Every day below has a DoD — if you hit it, stop and move on, even if the code isn't beautiful.

### Starting protocol sketch (finalize your own version on Day 1)

Plain text, newline-delimited, one command per line:

```cmd
SET <key> <value...>       -> OK
SET <key> <value...> EX <seconds>  -> OK
GET <key>                  -> VALUE <value>  |  NOT_FOUND
DEL <key>                  -> DELETED  |  NOT_FOUND
EXISTS <key>                -> TRUE  |  FALSE
EXPIRE <key> <seconds>      -> OK  |  NOT_FOUND
TTL <key>                   -> TTL <seconds_remaining>  |  NO_TTL  |  NOT_FOUND
PING                        -> PONG
```

Design notes to think through yourself on Day 1, not skip past:

- `<value...>` = everything after the key to end of line, so values can contain spaces. Keys can't (acceptable MVP restriction).
- Values can't contain newlines with this scheme — that's a known limitation, not a bug. Note it in your README; solving it properly (length-prefixed frames) is a stretch goal.
- Decide your error format now (`ERROR <message>`) so every command path can reuse it.

### Suggested repo layout

```cmd
raydash/
  cmd/
    server/main.go
    cli/main.go        # tiny Go client for manual testing
  internal/
    store/              # map + mutex, TTL logic — no networking, fully unit-testable
    protocol/            # parse a line -> command, serialize a response
    server/               # TCP accept loop, connection handling, dispatch
  client-java/
    src/...              # RaydashClient.java + a tiny cache-aside demo
  docs/
    PROTOCOL.md
  Dockerfile
  docker-compose.yml       # raydash + postgres + demo java app
  README.md
```

Keep `store` free of any networking code — that split is what makes it unit-testable and is genuinely how real systems are structured.

---

## Day 1 — Core store, no networking

**Build:** In-memory KV store as a plain Go package: `Set(key, value string)`, `Get(key string) (string, bool)`, `Delete(key string)`, backed by `map[string]string` behind a `sync.Mutex` (or `sync.RWMutex`). Write `docs/PROTOCOL.md` finalizing your wire format from the sketch above. Set up the repo structure and Go module.

**Definition of done:**

- [ ] `go test ./internal/store/...` passes with tests for set/get/delete, overwrite, get-missing-key, delete-missing-key
- [ ] `PROTOCOL.md` committed with every command, its exact request/response format, and error format
- [ ] Repo builds with `go build ./...`

---

## Day 2 — TTL and expiration

**Build:** Add an expiry timestamp per key. Implement **lazy expiration** (a `Get` on an expired key returns not-found and deletes it) and **active expiration** (a background goroutine that sweeps periodically, e.g. every 100ms, and removes expired keys — sample-based like Redis does, or a full sweep since your dataset is small, either is fine for MVP). Add `Expire(key, seconds)` and `TTL(key) (int, bool)` to the store.

**Definition of done:**

- [ ] Setting a key with a 1-second TTL and sleeping 1.5s makes `Get` return not-found
- [ ] A key with no TTL never expires
- [ ] Test proving active expiration actually removes the key from the map (not just that `Get` hides it) — check internal size or add a debug count method
- [ ] `go test -race ./...` is clean

---

## Day 3 — TCP server and protocol parsing

**Build:** `net.Listen` on a TCP port, accept loop, one goroutine per connection, `bufio.Reader` reading line by line, parser turning a line into a command + args per your `PROTOCOL.md`, dispatcher calling into the store, writer sending the response line back. Handle malformed input without crashing the connection or the server.

**Definition of done:**

- [ ] Server starts and listens on a configurable port
- [ ] You can connect with `nc localhost <port>` or `telnet`, issue `SET`, `GET`, `DEL`, `EXPIRE`, `TTL`, `PING` by hand, and get correct responses
- [ ] Malformed commands return an `ERROR` line, not a crash or dropped connection
- [ ] Closing the client connection cleanly closes the goroutine (no leak — verify with a quick connection-count check or just reasoning through the code)

---

## Day 4 — Hardening, more commands, Go CLI + integration tests

**Build:** Add `EXISTS`, `FLUSHALL`. Write `cmd/cli/main.go` — a minimal interactive TCP client for manual testing without netcat. Write integration tests that spin the server up on a random port, hit it over real TCP, and assert on responses. Stress-test with many concurrent connections hammering the same keys.

**Definition of done:**

- [ ] Go CLI connects and lets you run all commands interactively
- [ ] Integration test suite starts the server, runs a set of commands over TCP, asserts responses, and tears down cleanly
- [ ] A concurrency test (e.g. 50 goroutines doing SET/GET/DEL on overlapping keys) passes under `go test -race`
- [ ] No goroutine or connection leak after 100 connect/disconnect cycles (spot-check, doesn't need to be a formal test)

---

## Day 5 — Java client SDK + cache-aside demo

**Build:** `RaydashClient.java` — a thin wrapper over `java.net.Socket` with `connect()`, `set()`, `get()`, `del()`, `expire()`, `ttl()`, each doing exactly what your CLI does: write a line, read a line, parse it. Write a small demo Java app implementing cache-aside against a toy "database" — either a real Postgres table or, if time is short, an in-memory map with an artificial `Thread.sleep()` to simulate DB latency (pick based on how Day 1-4 went; don't let this become the reason the week slips).

**Definition of done:**

- [ ] Java demo: on a request, checks Raydash first; on miss, hits the "DB", stores the result in Raydash with a TTL, returns it
- [ ] Second request for the same key visibly skips the DB (log it) and comes back faster
- [ ] `RaydashClient` handles a `NOT_FOUND`/`ERROR` response without throwing an unhandled exception

---

## Day 6 — Dockerize and wire it together

**Build:** `Dockerfile` for the Go server (multi-stage build — compile in one stage, run the static binary in a minimal `scratch`/`alpine` image in the next). `docker-compose.yml` bringing up Raydash, Postgres (if you went that route on Day 5), and the Java demo on one network. Add graceful shutdown (SIGTERM closes listener, lets in-flight commands finish). Add basic config via env vars (port, active-expiry interval).

**Definition of done:**

- [ ] `docker-compose up` brings up all services with one command on a clean machine
- [ ] Java demo container can reach Raydash by service name (not `localhost`) inside the Docker network
- [ ] `docker-compose down` / `Ctrl+C` shuts down cleanly, no hung containers
- [ ] README has: what this is, how to run it, the protocol at a glance, and known limitations

---

## Day 7 — Load test, polish, stretch goals

**Build:** Simple load test — a Go script or your CLI in a loop doing thousands of SET/GET across many goroutines, timing it, sanity-checking throughput. Fix whatever breaks. Polish README and `PROTOCOL.md`. If time remains, pick from stretch goals below in priority order.

**Definition of done:**

- [ ] Load test runs and you can state a rough number (e.g. "~X ops/sec on my machine with N concurrent clients") — doesn't need to be fast, needs to be measured
- [ ] Fresh clone + `docker-compose up` works end to end per the README, on the first try
- [ ] At least one stretch goal attempted if Days 1-6 finished on schedule

### Stretch goals (only after Day 6 DoD is met — in priority order)

1. **Max-keys eviction** — cap the store size, evict oldest or random key on insert past the cap (simplest possible "not OOM" story; true LRU is a nice-to-have on top)
2. **Snapshot persistence** — dump the map to a file on shutdown/interval, reload on startup (crude but real)
3. **`/stats` or `INFO` command** — key count, uptime, ops served
4. **Binary-safe values** — solve the newline-in-value limitation via length-prefixed frames instead of line-delimited text
5. **AUTH token** — a single shared-secret check on connect

---

## Explicitly out of scope for this week

RESP protocol compatibility, clustering/replication, pub/sub, TLS, complex eviction policies beyond max-keys, a query language beyond the commands above. All reasonable v2 ideas — none of them belong in a 7-day MVP.

## Watch-outs specific to this project

- **TCP is a byte stream, not a message stream.** A single `Read()` can return a partial line or multiple lines glued together. `bufio.Reader.ReadString('\n')` handles this correctly — don't hand-roll buffer splitting.
- **Every goroutine you spawn per connection needs a clear exit path.** A client that connects and never sends anything, then disconnects, should not leak a goroutine — make sure your read loop returns on EOF/error.
- **Run `go test -race` from Day 2 onward**, not just once at the end. Data races in the store are the most likely bug class here and are far cheaper to catch early.
- **TTL tests are flaky if you're not careful** — use short TTLs (100-500ms) with a small sleep buffer, don't assert on exact millisecond timing.

---
