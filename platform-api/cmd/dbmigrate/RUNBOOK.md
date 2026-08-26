# dbmigrate — Runbook

One-time, offline migration of a Platform API **v1** PostgreSQL database into a fresh **v2**
database (core + EventGateway plugin). Build and run from the pinned v2 revision
(`a2911a091…`, see MIGRATION_MAPPING.md).

## 0. Build

```sh
cd <path-to>/platform-api        # the v2 repo root
go build -o dbmigrate ./cmd/dbmigrate/
```

## 1. Back up (non-destructive; v1 is the rollback point)

The tool never writes to v1. Keep the v1 dump (`choreo-dev-v1-backup.sql`) as the rollback point.

## 2. Stand up the databases

**v1 (source)** — restore the dump into an isolated Postgres on host `:5432`:

```sh
docker run -d --name mig-v1 -e POSTGRES_PASSWORD=admin -e POSTGRES_DB=dbv1 \
  -v "$PWD/choreo-dev-v1-backup.sql:/docker-entrypoint-initdb.d/backup.sql:ro" \
  -p 5432:5432 postgres:15
```

**v2 (target)** — a Postgres on host `:5433` whose v2 schema **already exists**. The tool does
**not** create the schema; apply both the v2 core DDL **and** the EventGateway plugin DDL
manually before running (the plugin DDL is *not* auto-applied for Postgres by the product, so
missing it will fail the WebSub/WebBroker artifacts):

```sh
docker run -d --name mig-v2 -e POSTGRES_PASSWORD=admin -e POSTGRES_DB=dbv2 \
  -p 5433:5432 postgres:15
# then apply, in order:
#   internal/database/schema.postgres.sql                 (v2 core DDL)
#   plugins/eventgateway/schema/schema.postgres.sql       (EventGateway plugin DDL)
```

(Alternatively use the `platform-api-v1` / `platform-api-v2` compose stacks, which seed the v2
core + plugin schema for you.)

### Handle width (REQUIRED before the backfill)

v2 declares `handle` as `VARCHAR(40)` and its code caps *new* handles at 40. v1 handles are
validated slugs up to **63** chars, and the migrator **preserves carried handles VERBATIM**
(no truncation) so external references stay stable — both v1 and v2 resolve `GET /…/{id}` by
handle, so a shortened handle would 404 a client that stored it. To hold a handle longer than
40 during the backfill, widen the carried-handle columns to `VARCHAR(255)` (matching v1)
**before** steps 5–6:

Apply the widen DDL (the 9 carried-handle columns) — `cmd/dbmigrate/ddl/01-widen-handle-to-255.sql`:

```sh
docker exec -i mig-v2 psql -U postgres -d dbv2 < cmd/dbmigrate/ddl/01-widen-handle-to-255.sql
```

Widening is metadata-only in Postgres (instant, no table rewrite; `UNIQUE(org, handle)` indexes
are unaffected). The generated-handle tables (`projects`, `subscription_plans`, `gateways`,
`api_keys`) keep `VARCHAR(40)` — their handles are derived from names and are always ≤40. The
committed `schema.postgres.sql` stays at `VARCHAR(40)` (v2-native); this widening is a
migration-window maneuver, reverted by the conformance gate in **step 8**.

## 3. Determine the source timezone (dbv1)

v1 stores audit timestamps as **naive `TIMESTAMP`** (no zone). The migrator reinterprets each
value as wall-clock time in `-source-tz` and writes the resulting **UTC instant** into v2's
`TIMESTAMPTZ` columns. If `-source-tz` is wrong, **every** migrated timestamp is silently shifted
by the zone offset — and `verify` only proves migrate and verify used the *same* zone, not that the
zone is *correct*. So establish it here, before any run. `-source-tz` is **required** (no default)
precisely so this decision is never skipped.

Determine the zone the original v1 platform-api wrote in (not the restored container's session
zone — the columns are zone-less, so `SHOW timezone` on `mig-v1` is only a weak hint):

1. **How v1 was deployed** — the authoritative source. WSO2 API Platform v1 services write UTC by
   default, but confirm against the real v1 deployment: its container/host `TZ`, the original DB
   server `timezone` setting, and the deployment region.
2. **Corroborate a known value** — pick a row whose true creation time you know from an external
   record and check which zone reproduces that wall-clock:

   ```sh
   docker exec mig-v1 psql -U postgres -d dbv1 -c \
     "SELECT name, created_at FROM artifacts ORDER BY created_at DESC LIMIT 5;"
   docker exec mig-v1 psql -U postgres -d dbv1 -c "SHOW timezone;"   # weak hint only
   ```

Pick the exact IANA zone (e.g. `UTC`, `Asia/Colombo`) and use that **same value** for both the
migrate run and the verify run below. If the v1 deployment ran in UTC (the product default) and
nothing above contradicts it, use `UTC`.

```sh
export SRC_TZ=UTC        # the zone confirmed above; passed to both migrate and verify
```

## 4. Set the subscription-token key

v2's `security.encryption_key` must be the value v1 **actually used** for
`subscription_token` (`database.subscription_token_encryption_key`, else `auth.jwt.secret_key`).
The tool reads it from the environment (never a flag):

```sh
export APIP_MIGRATION_ENCRYPTION_KEY="<the exact 32-byte key v1 used, hex(64) or base64>"
```

If v1 ran on the ephemeral fallback, those tokens are unrecoverable and must be re-issued.

## 5. Dry run (transform + validate, NO writes)

```sh
V1="postgres://postgres:admin@localhost:5432/dbv1?sslmode=disable"
V2="postgres://postgres:admin@localhost:5433/dbv2?sslmode=disable"
OUT=./migration-out             # any writable dir for run artifacts (report/quarantine/flags)
# $SRC_TZ was exported in step 3

./dbmigrate migrate -v1-dsn "$V1" -v2-dsn "$V2" -out-dir "$OUT" -run-id prod -source-tz "$SRC_TZ" -dry-run
```

Review, in `$OUT`:
- `migration-report-prod-dryrun.json` → `dropped_config_fields` (§E): **any field that carries
  data is a STOP** — remap before the live run.
- `quarantine-prod-dryrun.jsonl` → decide each row (fix source, or sign off as loss).
- `flags-prod-dryrun.jsonl` → every placeholder / synthesized value, and every carried handle
  that exceeds the v2-native 40 cap (`HANDLE_EXCEEDS_NATIVE_CAP`) — this is the exact set that
  will block the step 8 shrink gate.

The dry run needs no key; add `-skip-decrypt-check` if `APIP_MIGRATION_ENCRYPTION_KEY` is unset.

## 6. Live run

```sh
./dbmigrate migrate -v1-dsn "$V1" -v2-dsn "$V2" -out-dir "$OUT" -run-id prod -source-tz "$SRC_TZ"
```

Idempotent and resumable: re-run the same command after fixing source data or an interruption
(ON CONFLICT DO NOTHING + the file checkpoint keep handles stable and rows de-duplicated).
`-run-id` is **required** and must be the **same value** across the migrate run, any resume, and
the verify below — it names all run artifacts (`migration-state-<id>.json`, `quarantine-<id>.jsonl`,
etc.), so resume finds the checkpoint and verify finds the run it is checking.

## 7. Verify (read-only gate; non-zero exit on FAIL)

```sh
./dbmigrate verify -v1-dsn "$V1" -v2-dsn "$V2" -out-dir "$OUT" -run-id prod -source-tz "$SRC_TZ"
```

`-source-tz` **must be the exact same value** used for the migrate run — verify re-derives the
UTC instants to compare, so a different zone here would produce spurious mismatches.

Writes `verify-report-prod.json`. The gate passes only when every table reconciles
(`v2 + quarantine == v1`), every transform round-trips, and every quarantined key is resolved in
v2 or listed in `quarantine-signoff.jsonl` ({source_table, source_key} per line).

No `APIP_CP_ENCRYPTION_KEY` is needed (the WebSub HMAC table stays empty, §K.3).

## 8. Conformance gate — shrink `handle` back to 40 (post-verify)

After verify passes, attempt to restore the v2-native width. This `ALTER` is the **gate** that
surfaces any handle exceeding 40 — Postgres fails the shrink loudly if any value is too long:

```sh
# Succeeds iff EVERY carried handle fits 40; else ERROR: value too long for type character varying(40).
# 02-shrink-handle-to-40.sql wraps the ALTERs in a transaction so a failure rolls back cleanly.
docker exec -i mig-v2 psql -U postgres -d dbv2 < cmd/dbmigrate/ddl/02-shrink-handle-to-40.sql
```

- **Passes** → the DB is byte-consistent with a native v2 (all handles ≤40). Done.
- **Fails** → at least one handle exceeds 40. Enumerate them (also flagged
  `HANDLE_EXCEEDS_NATIVE_CAP` in `flags-<id>.jsonl`):

  ```sh
  docker exec -i mig-v2 psql -U postgres -d dbv2 < cmd/dbmigrate/ddl/handle-over-40-report.sql
  ```

  Then choose: **(a) keep the column wide** (leave at 255, or settle on 63 = v1's real cap) —
  v2 tolerates >40 handles at runtime (create/update validate 40, but `GET …/{handle}` has no
  length check), so everything keeps resolving; or **(b) rename the offending artifacts** to ≤40
  in v1 and re-run migrate (idempotent), then re-attempt the shrink. Do **not** truncate
  in-place — that breaks the handle-based external references this preservation protects.

**Dual-write note:** while the live dual-writer is enabled it also preserves handles verbatim,
so keep the column at `VARCHAR(255)` for the whole dual-write window; only run this shrink gate
at/after cutover.

## Flags of note

| flag | default | purpose |
|---|---|---|
| `-run-id` | _(required)_ | stable id naming all run artifacts; must match across migrate / resume / verify |
| `-dry-run` | off | transform+validate, no writes |
| `-source-tz` | _(required)_ | IANA zone the naive v1 TIMESTAMP values were written in (determine first, step 3); must match across migrate / verify |
| `-skip-decrypt-check` | off | skip the mandatory token decrypt guard (dry-runs only) |
| `-populate-artifact-subscription-plans` | off | derive the (redundant) artifact_subscription_plans rows |
| `-audit-marker` | off | emit one "migrated" audit row per org |
| `-migration-epoch` | `2026-01-01T00:00:00Z` | fixed epoch for deterministic synthesized UUIDs |
