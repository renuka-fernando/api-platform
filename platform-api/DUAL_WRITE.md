# Dual-Write Intermediate Build (v1 → v2 DB bridge)

This build of Platform API **v1** keeps its v1 REST contract and v1 database unchanged, but —
when enabled — **additionally mirrors every mutation into the v2 database** through the shared
[`migrationcore`](../../api-platform-migration/platform-api/migrationcore) package (the same code
the batch migrator `dbmigrate` uses). v1 stays the source of truth and the rollback point; v2 is
kept warm for a later cutover.

- **Reads are always served from v1.** Only writes are mirrored.
- **v1 writes first and commits; the v2 write is a non-blocking follow-on.** A v2 failure never
  rolls back or affects the v1 operation, and **never reaches the REST client**.
- **Off by default** — with `dual_write.enabled = false` this is byte-for-byte stock v1 (no v2
  connection is opened).

## Configuration

```toml
[dual_write]
enabled       = false                   # master switch; false ⇒ stock v1
write_timeout = "2s"                     # bounds the in-line v2 write
epoch         = "2026-01-01T00:00:00Z"  # MUST equal the batch's -migration-epoch
source_tz     = "UTC"                    # MUST equal the batch's -source-tz
failure_log   = "./data/v2_dual_write_failures.jsonl"  # append-only JSONL; NOT a v1-DB table

[dual_write.database]                    # v2 target DB — same shape as [database]
type     = "postgres"
host     = "..."
port     = 5432
name     = "platform_api_v2"
user     = "..."
password = "..."
ssl_mode = "disable"
```

Every key also has an environment-variable form: `DUAL_WRITE_ENABLED`, `DUAL_WRITE_WRITE_TIMEOUT`,
`DUAL_WRITE_EPOCH`, `DUAL_WRITE_SOURCE_TZ`, `DUAL_WRITE_FAILURE_LOG`, and `DUAL_WRITE_DATABASE_*`
(host/port/name/user/…).

Validation (at config load) is **structural only** when enabled: `write_timeout > 0`, a non-zero
RFC3339 `epoch`, a valid `source_tz`, and a v2 `database.driver`. Connectivity is **not** checked at
config load — see *Startup behavior*.

### Continuity requirements (do not get these wrong)

| Invariant | Why |
|---|---|
| `dual_write.epoch` == the batch's `-migration-epoch` (default `2026-01-01T00:00:00Z`) | The epoch seeds the **deterministic synthesized audit-identity UUIDs** (`user_idp_references`). A divergent epoch splits an actor into two identities between the backfill and the live path. |
| `dual_write.source_tz` == the batch's `-source-tz` (default `UTC`) | v1 stores tz-naive `TIMESTAMP`s; the source tz reinterprets them to UTC instants. A divergent tz shifts every mirrored timestamp. |
| The v2 DB's subscription-token **encryption key** == the key v1 uses | Subscription tokens are already encrypted by v1 and pass through to v2 **verbatim**. Set the v2 key to v1's actual key **before** the backfill, and **do not change v1's key** when enabling dual-write. (The mirror does not re-encrypt; `migrationcore.Options.EncryptionKey` is unused on the write path — it exists only for the batch's decrypt guard.) |

### Schema ownership

- **Do NOT initialize the v2 schema from this build.** `dbmigrate` owns the v2 core + plugin DDL.
- **The mirror never writes to the v1 database.** Its only v1 access is the read-back (read-only), and
  its failure bookkeeping goes to an **append-only JSONL file** (`dual_write.failure_log`), NOT a v1-DB
  table. So the v1 database stays byte-for-byte pristine — the rollback point is untouched.

## Startup behavior (v2 never blocks v1)

When `enabled`, the server opens the v2 pool **without a fatal ping**:

- v2 reachable → decorators installed; mirroring active.
- v2 **unreachable** → a loud `ERROR` (marker `V2_DUAL_WRITE_FAILURE`) is logged, **v1 boots and serves
  normally**, and mirror writes are recorded as failures until v2 returns (`database/sql` reconnects
  transparently, so the mirror self-heals). This deliberately overrides a strict "v2 must be connectable"
  reading of the config in favor of the prime directive: **a v2 problem must never stop v1**.
- v2 **misconfigured** (bad driver / unparseable epoch or tz) → logged; the server runs **stock v1** (no
  wrapping) for that boot.

## The single failure model (synchronous, non-blocking)

There is exactly one behavior — no `failure_mode` switch. Right after the v1 write commits, the mirror runs
in a v2 transaction bounded by `write_timeout` (enforced at the connection level via `statement_timeout`
+ `connect_timeout`, because `migrationcore.Execer` has no `context`). On **any** v2 error or timeout, the
mirror:

1. logs at **ERROR** with the marker `code = "V2_DUAL_WRITE_FAILURE"` (also a greppable literal), plus
   `entity, op, table, uuid, org, err`; and
2. appends a durable JSON line to the **`dual_write.failure_log`** file — `{code, entity, op, table, key,
   org, error, occurred_at}` — never to the v1 DB; then
3. **returns the v1 result unchanged.** The REST client only ever sees the v1 outcome.

The marker `V2_DUAL_WRITE_FAILURE` is the one string that spans **logs → the failure log → the reconcile
runbook**, so you can filter/alert/replay with a single token.

> **HA note:** the failure log is a per-process file, so each replica writes its own. Point
> `dual_write.failure_log` at a per-replica persistent volume and gather all of them when reconciling.
> A shared file across replicas is unsafe (interleaved appends). The v2 DB is not used for failures — its
> unreachability is the very thing being recorded.

> Operational trade-off: mirroring is synchronous with no circuit breaker (a deliberate, pinned decision).
> During a v2 outage each mutation adds up to `write_timeout` of latency and appends a failure row; the
> `connect_timeout` bounds the added latency, and reconcile (below) heals the backlog once v2 returns.

## Reconciliation

The JSONL failure log is replayed with the migration client's targeted reconcile (added in `dbmigrate`
for exactly this). `dbmigrate migrate -only-keys` accepts either `<op> <table> <key>` lines OR the JSONL
failure log **verbatim** (each line is a JSON object with `op`/`table`/`key`), so no transformation is
needed:

```sh
# 1. (HA) gather every replica's failure log into one file.
cat /mnt/dw-logs/*/v2_dual_write_failures.jsonl > all-failures.jsonl

# 2. Replay through the SAME per-row UpsertX/DeleteX (live semantics: DO UPDATE).
dbmigrate migrate -v1-dsn "$V1_DSN" -v2-dsn "$V2_DSN" -out-dir ./out -run-id reconcile \
  -only-keys all-failures.jsonl

# 3. After verifying, rotate the logs aside so they are not re-processed (append-only; replaying is
#    idempotent anyway, so double-processing is harmless).
for f in /mnt/dw-logs/*/v2_dual_write_failures.jsonl; do mv "$f" "$f.$(date +%s).done"; done
```

- **`upsert`** keys re-run the row from v1 through the shared `UpsertX` (idempotent `ON CONFLICT DO UPDATE`).
- **`delete`** keys are replayed through `DeleteX`, because an upsert-reconcile can never heal a delete (the
  v1 row is already gone). Composite keys are `|`-joined in `DeleteX` argument order (e.g. `deployment_status`
  = `<org>|<artifact>|<gateway>`); the decorators write exactly that form.
- **Enable-vs-backfill gap / full re-sync:** `dbmigrate migrate … -since <ts>` re-runs every row idempotently
  (live semantics), healing any window where dual-write was off or degraded.

Reconcile is safe to run repeatedly: every write is idempotent, `created_at`/`created_by` are immutable on
update, and superseded child rows (gateway endpoint, plan limit) are replaced — no duplicates, no clobber,
no stale children.

## Convergence guarantee (how the mirror stays faithful)

Each decorator, after the v1 write, **reads the raw v1 columns straight back from the v1 DB** using the same
SELECTs the batch iterator uses, then feeds them to `migrationcore` — rather than reconstructing from the
domain model. This sidesteps every model/stored mismatch (the encrypted+hashed subscription token, the
bundled LLM-template config, the `"{}"` gateway-properties default, the manifest that is not on the Gateway
model) and guarantees the live path produces **byte-for-byte what a fresh batch of the final v1 state would**
— so `dbmigrate verify` converges. Handles for handle-less entities (projects, gateways, plans, api-keys) are
generated with the product's deterministic slug; a rare slug **collision** would get a random suffix in the
batch and cannot be reproduced exactly — treat that as the one narrow non-convergence edge.

## Cutover choreography

1. Create an empty v2 DB; set its subscription-token key to v1's key.
2. `dbmigrate migrate` — bulk backfill v1 → v2.
3. **Deploy THIS build with `dual_write.enabled = true`.** v1 keeps serving; every mutation now also lands in
   v2. Alert on the `V2_DUAL_WRITE_FAILURE` marker; reconcile as needed.
4. Port the v2 REST APIs into the codebase.
5. Cut reads over to v2 and retire v1.

To roll back at any point before step 5: set `dual_write.enabled = false` and redeploy — v1 is untouched and
authoritative.

## Module dependency note

This build imports `migrationcore` from the **v2 module** via a local `replace` in `src/go.mod`
(`github.com/wso2/api-platform/platform-api => …/api-platform-migration/platform-api`, plus a
`github.com/wso2/go-httpkit` replace that `migrationcore`'s transitive deps require). A local `replace`
tracks the v2 **working tree**, not a pinned commit — check out v2 at the intended commit (`a2911a09…`) before
building so the intermediate is reproducible. Importing `migrationcore` raises a few shared deps by
Minimum-Version-Selection (validator, mapstructure/v2, oapi-codegen/runtime, gorilla/websocket, x/crypto); the
full v1 build + tests were re-run after wiring to confirm no regression. The v1 `go` directive (and the repo
`go.work`) were bumped to 1.26.5 to match v2.
