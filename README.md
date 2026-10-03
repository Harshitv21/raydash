# README

Raydash is a self-deployable mini cache built in Go, featuring Springboot support
via a custom written java client: [raydash-client-java](https://github.com/Harshitv21/raydash-client-java/).

I built this project for a couple of reasons, but the biggest one is that Redis
kept threatening to delete the database I created for my other project, [staycultured.app](https://staycultured.app).
It's an all-in-one media tracker for games, movies, tv shows & anime. Because it
is currently a work in progress and I can't work on it full-time, the cache occasionally
sits inactive. Whenever that happens Redis sends me a threat and has wiped my database
a couple of times which is quite frustating honestly.
Now the "quick fix" for this problem was simple, whenever an email arrived I manually
hit an endpoint via the [Swagger OpenAPI docs](https://api.staycultured.app) to
keep it alive but man I couldn't be bothered with this shit.

On top of that, the HUGE free **30MB limit** on Redis free tier was pretty restrictive
for my use case. My average API responses hover on the very least at around 30-50KB,
which only grants me room for about <u>~1,000 JSON objects</u>. If an API like Jikan
(for anime) or TMDB returns a massive payload, that limit is gone with realistically
5-10 users using my app! (no one is using it right now I am just saying in hypothetical
scenarios...). Upgrading means paying monthly fees of _5ish$_ dollars or something
for _slightly_ bigger limits.

Since I am already paying a cloud provider for deployments anyways, I figured why
not host my own cache? or even better why not built my own cache? I get full control
over my data and save some cash too! Plus I get a fantastic excuse to learn **Go**
and build a cool project with it!

What you see here is a bare-minimum, self deployable mini-cache. While it doesn't
have advanced enterprise features (YET), it gets the job done perfectly for simple
use cases AND it's a really great project for diving into advanced backend concepts~!

**At a glance**,

- Binary safe protocol
- Optional AUTH
- Graceful shutdown
- Ready to use client support (Spring Boot)
- Optional Postgres persistence

## Table of Contents

| **Index** | **Sections** |
| :--- | :--- |
| 1 | [Running locally](#running-locally) |
| 2 | [Configuration](#configuration) |
| 3 | [Architecture](#architecture) |
| 4 | [Protocol](#protocol) |
| 5 | [Persistence](#persistence) |
| 6 | [Commands](#commands) |
| 7 | [Benchmark](#benchmark) |
| 8 | [Clients](#clients) |
| 9 | [Demos](#demos) |
| 10 | [Self hosting / Deployment](#self-hosting--deployment) |
| 11 | [Limitations](#limitations) |
| 12 | [Test Suite](#test-suite) |
| 13 | [Contributions](#contributions) |
| 14 | [License](#license) |
| 15 | [Links](#links) |

## Running locally

```cmd
docker compose up
```

For cli tool

```cmd
go run cli/main.go -server <any-valid-server-address> -auth <auth-token>
```

`docker compose up` will spin up both the services together in a coupled container
but alternatively you can run the server and postgres db separately for testing
and benchmarking without the docker network overhead.

```cmd
go run ./internal/main.go
```

Make sure to have a `.env` file present in the root of this project (find more at
[Configuration](#configuration) below). This might be a good time to remind that
persistence and authentication (via an auth token) are optional.

## Configuration

Right now variables under `environment` section of `raydash` & `postgres` services
in `docker-compose.yml` are hardcoded but alternatively you can make a `.env`
file (make sure to `.gitignore` it) and docker automatically looks for the file
and loads the variables.

Example of `.env` file content,

```dotenv
# postgres
POSTGRES_DB=raydash
POSTGRES_USER=raydash
POSTGRES_PASSWORD=raydash

# raydash
RAYDASH_PORT=13203
RAYDASH_EXPIRY_INTERVAL_MS=100
RAYDASH_POSTGRES_DSN=postgres://raydash:raydash@postgres:5432/raydash?sslmode=disable
RAYDASH_SNAPSHOT_INTERVAL_SECONDS=30
RAYDASH_MAX_KEYS=0
RAYDASH_AUTH_TOKEN=changeme
```

Usage in `docker-compose.yml`,

```yaml
environment:
    POSTGRES_DB: ${POSTGRES_DB}
    POSTGRES_USER: ${POSTGRES_USER}
    POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}

# Similarly the rest
```

For local setup without docker we do the same thing, make a `.env` file in the
root, write the variables and with the help of `godotenv` package in `internal/main.go`
it will load those variables automatically.

Flags for the [loadtest](internal/loadtest/main.go) are
hardcoded in case none are provided but you can look at the source code and modify
accordingly.

## Architecture

How does this whole project work?

![Architecture Diagram](./diagrams/architecture.png)

## Protocol

![Protocol Diagram](./diagrams/protocol.png)

## Persistence

Persistence is optional. To enable it you must pass a valid postgres DSN. In case
of no valid DSN Raydash will run pure in memory. Running locally without docker
meaning leaving the `RAYDASH_POSTGRES_DSN` blank or absent in `.env` file.

Making persistence optional using docker is a bit of work. Since compose spins up
both `raydash` and `postgres` service together and `raydash` service heavily refers
to it do these things to run without a database:

- Remove the `postgres` service completely. Delete the `postgres` block in under
  services.
- Remove the `RAYDASH_POSTGRES_DSN` line from `raydash` service or leave it blank.
- Remove the `depends_on` block from `raydash` service. If you don't remove it
  Docker will throw an error and refuse to start.
- Lastly delete the `raydash-pgdata` from the `volumes:` block at the very bottom
  of the `docker-compose.yml` so you don't leave unused storage definition behind.

Now let's look at how persistence works,

![Persistence explaination diagram](./diagrams/persistence.png)

In case you want to have persistence for a native run for whatever weird reason
then you have 2 options:

- Run `docker compose up postgres` and run only the `postgres` service. Then add
  DSN for the exposed db to our `.env` variable and you have a docker running db
  acting as persistence.
- Other not so straightforward method is creating a database in the default postgres
  server (instructions listed down below) and enter DSN for that database which
  will also make it persist.

  - You can check if you have EDB Postgres (Community Version) installed or not
    by running `Get-Service *postgres*` in Powershell and if its present it will
    give us `Stopped` or `Running` depending on the current status. In case its
    not installed at all it will throw a `ServiceCommandException` or something
    like that.
  - If you want a GUI approach then `Windows + R`, type `services.msc` find the
    process, `postgresql-x64-16 - PostgreSQL Server 16` then you can `Start` it
    if its not in `Running` state and if its not present at all then you have to
    install EDB Postgres first.

## Commands

> Try `INSANE` and `HATE` for yourself 😃!

### `INFO`

```cmd
INFO
```

### `GET`

Get a key.

```cmd
GET <key>
```

### `SET`

> [!IMPORTANT]
> `SET` command is different from others since its server syntax requires 2 lines:
>
> ```cmd
> SET <key> <byte-length>\r\n
> <exact-byte-length-of-characters>\r\n
> ```
>
> Since this cache is specifically built to be used with a client like our [Java](#links)
> one you never use `SET` manually instead the client handles it by itself. **BUT**
> there is an exception! when you use the cli you **HAVE** to send the command in
> a single line there is simply no other option or at least not one which does not
> cause me a headache to build so what I do is I take the `SET` command using the
> format given down below and internally transform it to what the server expects.
> This way for the rare use case of cli tool it remains simple while following server
> protocol.

Binary safe format for setting a key-value pair.

```cmd
SET <key> <value>
```

### `DEL`

Remove a key.

```cmd
DEL <key>
```

### `EXPIRE`

Sets expire time for a key (in seconds).

```cmd
EXPIRE <key> <time-in-seconds>
```

### `TTL`

Return remaining time-to-live given a valid key.

```cmd
TTL <key>
```

### `EXISTS`

Return `:0` if no key is present & `:1` if key is present.

```cmd
EXISTS <key>
```

### `FLUSHALL`

Clears the whole store use carefully! No arguments needed.

```cmd
FLUSHALL
```

### `AUTH`

To authenticate yourself with the Server. I doubt you will ever use this as a
standalone command this is usually configured if present and auto sent before any
other command but still good to know.

```cmd
AUTH <auth-token>
```

## Benchmark

### Native Performance

These metrics represent the application running directly on the host machine
(without Docker or containerization overhead).

| Metric | Value |
| :--- | :--- |
| **Throughput** | 49,026 ops/sec |
| **Total Operations** | 491,106 (245,553 **`SET`** / 245,553 **`GET`**) |
| **Errors** | 0 (0.00%) |
| **Avg/p50 Latency (`SET`)** | 1.02 ms |
| **Avg/p50 Latency (`GET`)** | 939.5 µs |
| **p99 Latency (Combined)** | 3.45 ms |

### Docker container deployed performance benchmarks

| Metric | Value |
| :--- | :--- |
| **Throughput** | 1,990 ops/sec |
| **Total Operations** | 20,040 (10,020 **`SET`** / 10,020 **`GET`**) |
| **Errors** | 0 (0.00%) |
| **p50 Latency (`SET`)** | 46.10 ms |
| **p50 Latency (`GET`)** | 2.33 ms |
| **p99 Latency (Combined)** | 51.75 ms |

### Deployed Performance (Best Run - railway.app SEA Server)

These metrics represent the application deployed on a remote server (located in
Southeast Asia/Singapore).

| Metric | Value |
| :--- | :--- |
| **Throughput** | 805 ops/sec |
| **Total Operations** | 8,136 (4,068 **`SET`** / 4,068 **`GET`**) |
| **Errors** | 0 (0.00%) |
| **p50 Latency (`SET`)** | 58.17 ms |
| **p50 Latency (`GET`)** | 57.86 ms |
| **p99 Latency (Combined)** | 84.15 ms |

### Environment Performance Comparision

A side-by-side breakdown of application performance across native execution, local
containerization (Docker Compose), and a remote cloud deployment.

| Metric | Native Performance | Docker Compose (Local) | Deployed Server (Railway SEA) |
| :--- | :--- | :--- | :--- |
| **Throughput** | 49,026 ops/sec | 1,990 ops/sec | 805 ops/sec |
| **Total Operations** | 491,106 | 20,040 | 8,136 |
| **Workload Split** | 245,553 (**`SET`** / **`GET`**) | 10,020 (**`SET`** / **`GET`**) | 4,068 (**`SET`** / **`GET`**) |
| **Errors** | 0 (0.00%) | 0 (0.00%) | 0 (0.00%) |
| **p50 Latency (`SET`)** | 1.02 ms | 46.10 ms | 58.17 ms |
| **p50 Latency (`GET`)** | 939.5 µs | 2.33 ms | 57.86 ms |
| **p99 Latency (Combined)** | 3.45 ms | 51.75 ms | 84.15 ms |

Quick _obvious_ takeaways:

- **Bare Metal Dominance 🥵:** Running natively delivers **maximum** results with
  sub-milliseconds to low-milliseconds latencies, this is the **TRUE** baseline
  capacity of this application btw. Every other stat simply **DOES NOT** matter.
  (the last statement is more _absolute_ than _true_, the difference in stats is
  more like "_how fast and optimized my code is_" vs "_what does a real deployed_
  _user experience_").
- **Docker Network Bottleneck:** Local Docker Compose shows a massive
  _96%_ drop in throughput and a huge spike in `SET` latency. This is directly due
  to Docker Desktop's VM-based network proxy (the port-forwarding layer on Windows/Mac)
  adding a flat per-connection tax, nothing to do with file I/O or volume mounts.
- **Network-Bound Cloud Deployment:** The Railway SEA instance throughput drops
  even further, but this is completely normal given the nature of cross-border
  network round-trip-time (RTT) from India to Singapore, meaning bottleneck here
  turns out to be the physical transit distance and **NOT** related to the
  application whatesover.

## Clients

### Spring Boot

- [Maven Repository][2]
- [Github Repository][3]

## Demos

### Spring Boot

- Start the raydash server using `docker-compose.yml`
- Run the springboot application from `demos/springboot/app/src/main/java/raydash/demo/app/AppApplication.java`
- `application.properties` is loaded with basic server configuration already
- In the root of this `app` you can find a `bruno` folder containing a basic `GET`
  endpoint for fetching user by id, double hit that endpoint (you'll need the `bruno`
  desktop client but if you prefer a straightforward way you can call it from curl
  or any other API client of your choice).
- Use pgadmin or any other database GUI to connect to our docker database using
  the DSN, `postgres://raydash:raydash@localhost:5432/raydash?sslmode=disable` and
  look for table, `raydash_snapshot` which will confirm cache hit.

## Self hosting / Deployment

Deploying could be pretty straightforward or might have some extra steps involved
depending on which provider you choose. My own deployment on railway.app is pretty
straightforward I link my github repository to a new service, create another one
for the database link the DSN, environment variables gets hardcoded from my `.env`
file and it automatically picks up the `Dockerfile`.

So in simple words `fork` or `clone` this repo, add that as a service it will take
care of the code compilation and build on itself (you might need to add instructions
for compilation yourself) then if you want persistence you have to add a postgres
database and connect them and that's it!

Most of the PaaS providers like railway.app have quick and easy steps for a repo
deployment ocassionally using the `Dockerfile`. But in case its more manual work
the one extra step you need for this cache is having a database connected to your
repository in case you want persistence and thats the part you have to figure yourself.

## Limitations

Known limitations worth pointing out:

- Snapshot persistence only starts working up to one `RAYDASH_SNAPSHOT_INTERVAL_SECONDS`.
  Any hard crash upto that point on in between snapshot intervals will lead to
  data loss.
- The **ENTIRE CONNECTION** and everything that travels over the wire is unencrypted!
  every single key and value (difference between _unauthenticated_ vs _unencrypted_).
- `RAYDASH_MAX_KEYS` caps the key count, there is no provision to cap the byte size.
- No GUI present for cache either you have to use cli or connect database to a GUI
  for database and go over the contents manually using SQL.

## Test Suite

> To run individual functions you can do that from you IDE itself (I can do that
> at least in vscode)

Test suite for `server.go` & `store.go` contains tests for core functionality like
concurrenct read/write, binary safe protocol, authentication, CRUD operations, server
related stuff etc.

### `server`

- `server_test.go`: Tests for concurrent read/write/delete operations with 50 workers.
- `server_protocol_test.go`: Probably the most important test file out of all. Tests
  for `AUTH`, `INFO` & `FLUSHALL` commands under multiple scenarios, validates our
  rewritten binary safe protocol with trick values.

Navigate from root to `server` directory,

```cmd
cd internal/server
go test .
```

Run with race condition,

```cmd
go test -race .
```

Run multiple times with race condition,

```cmd
go test -race -count=10 .
```

### `store`

- `store_bench_test.go`: Rough benchmarks under a simple environment for read/writes.
- `store_concurrency_test.go`: 100 writers and 100 readers running genuine concurrent
  operations against the same keys.
- `store_crud_test.go`: CRUD operations.
- `store_key_value_test.go`: `TTL` check along with lazy and active expiration of
  keys.
- `store_eviction_test.go`: Tests our `maxKeys` property under multiple conditions.

Navigate from root to `store` directory,

```cmd
cd internal/store
go test .
```

Run with race condition,

```cmd
go test -race .
```

Run multiple times with race condition,

```cmd
go test -race -count=10 .
```

## Contributions

You are welcome to add suitable new features or enhance existing ones 😃

## License

This project is licensed under the **MIT License**. See the [License](./LICENSE)
file for more details.

## Links

- [raydash.app][1]

---

[1]: https://raydash.app
[2]: https://mvnrepository.com/artifact/app.raydash/raydash-client-java
[3]: https://github.com/Harshitv21/raydash-client-java
