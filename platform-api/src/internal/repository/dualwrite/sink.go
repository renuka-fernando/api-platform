/*
 *  Copyright (c) 2026, WSO2 LLC. (http://www.wso2.org) All Rights Reserved.
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *  http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 *
 */

// Package dualwrite contains the live dual-write path of the intermediate v1 Platform API
// build (§6). Each mutating repository interface is wrapped by a thin decorator: reads
// delegate straight through to the real v1 repo (reads are ALWAYS served from v1), and
// mutations call the v1 repo first, then — on success — mirror the resulting state into
// the v2 database through the SAME shared migrationcore package the batch backfill uses.
//
// The v2 write is synchronous but strictly non-blocking (§6.5): it runs right after the v1
// write has already committed and is bounded by write_timeout; on ANY error or timeout it
// is logged at ERROR + recorded in v2_dual_write_failures, then swallowed. v1 stays the
// source of truth and the rollback point, so the REST response reflects only the v1 outcome.
package dualwrite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"platform-api/src/config"
	"platform-api/src/internal/database"

	_ "github.com/jackc/pgx/v5/stdlib" // v2 target is PostgreSQL (pgx stdlib)
	"github.com/wso2/api-platform/platform-api/migrationcore"
)

// MirrorFailure is the single stable marker stamped on every failed v2 mirror write. It
// spans the structured ERROR log (as the "code" field AND a greppable literal), the
// v2_dual_write_failures.code column, and the reconcile runbook, so an operator can
// filter → alert → replay a mirror failure with one token (§6.5).
const MirrorFailure = "V2_DUAL_WRITE_FAILURE"

// Sink bundles everything a decorator needs to mirror a v1 mutation into v2 (§6.1): the v2
// connection pool, the v1 connection (for the raw read-back that builds the migrationcore
// rows), the shared migrationcore Options + Reporter, and the write timeout. It is built
// once at startup and shared (read-only) by all decorators.
type Sink struct {
	v2           *sql.DB
	v2Postgres   bool
	v1           *database.DB // v1 connection — used ONLY for read-back (reads); never written to
	opts         migrationcore.Options
	reporter     migrationcore.Reporter
	writeTimeout time.Duration
	logger       *slog.Logger

	// failureLog is the append-only JSONL path; failMu serializes appends from concurrent
	// in-flight requests within this process.
	failureLog string
	failMu     sync.Mutex
}

// NewSink opens the v2 connection (non-fatally — see openV2Pool), ensures the v1-side
// v2_dual_write_failures table exists, and builds the shared migrationcore Options from the
// dual-write config (Epoch + SourceTZ pinned to the batch's values). It returns an error
// only for a misconfiguration the operator must fix (bad driver / unparseable epoch or tz);
// the caller treats that as "run stock v1, loudly" so a dual-write problem never blocks boot.
func NewSink(cfg *config.DualWrite, v1 *database.DB, logger *slog.Logger) (*Sink, error) {
	epoch, err := cfg.ParsedEpoch()
	if err != nil {
		return nil, fmt.Errorf("dual_write.epoch: %w", err)
	}
	loc, err := time.LoadLocation(cfg.SourceTZ)
	if err != nil {
		return nil, fmt.Errorf("dual_write.source_tz %q: %w", cfg.SourceTZ, err)
	}
	// Failure log is a FILE, not a v1-DB table, so the mirror never writes to the v1
	// database (v1 stays pristine). Ensure its directory exists.
	failureLog := strings.TrimSpace(cfg.FailureLog)
	if failureLog == "" {
		failureLog = "./data/v2_dual_write_failures.jsonl"
	}
	if dir := filepath.Dir(failureLog); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create dual_write.failure_log dir %q: %w", dir, err)
		}
	}
	v2, isPg, err := openV2Pool(&cfg.Database, cfg.WriteTimeout, logger)
	if err != nil {
		return nil, err
	}
	return &Sink{
		v2:         v2,
		v2Postgres: isPg,
		v1:         v1,
		failureLog: failureLog,
		opts: migrationcore.Options{
			// EncryptionKey is intentionally nil: migrationcore uses it only for the batch's
			// token decrypt guard, which the live path never runs. Subscription tokens are
			// already encrypted by v1 and pass through verbatim; the real continuity invariant
			// is that the v2 DB's key equals v1's (operator-set BEFORE the backfill).
			SourceTZ:           loc,
			Epoch:              epoch,
			InsertOnly:         false, // live UPDATE mirroring (ON CONFLICT DO UPDATE)
			SkipIdentityUpsert: false, // seed each audit actor into user_idp_references on demand
		},
		reporter:     reporter{logger: logger},
		writeTimeout: cfg.WriteTimeout,
		logger:       logger,
	}, nil
}

// openV2Pool opens the v2 pool WITHOUT a fatal ping: a v2 that is down at startup must
// never stop v1 from booting or serving (§3.3 overrides §4's "connectable"). database/sql
// reconnects transparently when v2 returns, so the mirror path self-heals; mutations during
// the outage are recorded in v2_dual_write_failures for later reconcile. connect_timeout
// bounds each connect attempt so a down v2 adds at most ~write_timeout to a mutation rather
// than hanging on the OS TCP timeout. Returns (pool, isPostgres, err); err is only for a
// misconfiguration (unsupported driver / bad DSN), never for unreachability.
func openV2Pool(cfg *config.Database, writeTimeout time.Duration, logger *slog.Logger) (*sql.DB, bool, error) {
	switch strings.ToLower(cfg.Driver) {
	case database.DriverPostgres, database.DriverPostgreSQL, database.DriverPGX:
		connectTimeout := int(writeTimeout.Seconds())
		if connectTimeout < 1 {
			connectTimeout = 1
		}
		dsn := fmt.Sprintf(
			"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s connect_timeout=%d",
			cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Name, cfg.SSLMode, connectTimeout,
		)
		db, err := sql.Open(database.DriverPGX, dsn)
		if err != nil {
			return nil, false, fmt.Errorf("open v2 dual-write pool: %w", err)
		}
		maxOpen := cfg.MaxOpenConns
		if maxOpen <= 0 {
			maxOpen = 10
		}
		maxIdle := cfg.MaxIdleConns
		if maxIdle <= 0 {
			maxIdle = 5
		}
		db.SetMaxOpenConns(maxOpen)
		db.SetMaxIdleConns(maxIdle)
		db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Second)
		if err := db.Ping(); err != nil {
			logger.Error("v2 dual-write DB not reachable at startup — v1 serves normally; mirror writes will be recorded as failures until v2 returns",
				slog.String("code", MirrorFailure), slog.Any("err", err))
		} else {
			logger.Info("v2 dual-write DB connection established", "host", cfg.Host, "port", cfg.Port, "dbname", cfg.Name)
		}
		return db, true, nil
	default:
		return nil, false, fmt.Errorf("dual_write.database.driver %q is unsupported (the v2 target must be postgres)", cfg.Driver)
	}
}

// mirrorUpsert mirrors a v1 create/update. entity is the log label (e.g. "rest_api"); table
// + key are the reconcile routing coordinates recorded in the failure log so a replay via
// `dbmigrate migrate -only-keys` targets the right iterator (table = the v1 source table,
// key = its PK or a "|"-joined composite in the guard's order).
func (s *Sink) mirrorUpsert(entity, table, key, org string, fn func(migrationcore.Execer) error) {
	s.mirror(entity, "upsert", table, key, org, fn)
}

// mirrorDelete mirrors a v1 delete. table + key must match a dispatchDelete case in
// cmd/dbmigrate (table routes to the DeleteX; key is "|"-joined in DeleteX argument order).
func (s *Sink) mirrorDelete(entity, table, key, org string, fn func(migrationcore.Execer) error) {
	s.mirror(entity, "delete", table, key, org, fn)
}

// mirror runs fn inside a bounded v2 transaction and, on ANY error or timeout, records a
// durable failure WITHOUT propagating (the v1 write is already committed and authoritative).
func (s *Sink) mirror(entity, op, table, key, org string, fn func(migrationcore.Execer) error) {
	if err := s.runTx(fn); err != nil {
		s.recordFailure(entity, op, table, key, org, err)
	}
}

// mirrorResolvedDelete mirrors a delete whose v2 key had to be resolved from a natural key
// BEFORE the v1 delete removed the row. A resolved uuid is mirrored normally; an empty uuid
// (resolution failed while the v1 delete still succeeded — a near-impossible race) is logged
// with the marker so an operator can reconcile manually, since no reconcilable key exists.
func (s *Sink) mirrorResolvedDelete(entity, table, uuid, org string, del func(migrationcore.Execer) error) {
	if uuid == "" {
		s.logger.Error("V2_DUAL_WRITE_FAILURE: could not resolve a uuid to mirror a delete — reconcile manually",
			slog.String("code", MirrorFailure), slog.String("entity", entity), slog.String("op", "delete"), slog.String("org", org))
		return
	}
	s.mirrorDelete(entity, table, uuid, org, del)
}

// mirrorResolvedUpsert mirrors an upsert whose v2 key (uuid) was resolved from a natural key
// — used by the artifact-type Update paths, which are keyed by handle. The failure row must
// carry the uuid (the reconcile upsert guard matches on uuid), so an unresolved uuid is
// logged with the marker rather than recorded under a non-reconcilable key.
func (s *Sink) mirrorResolvedUpsert(entity, table, uuid, org string, up func(migrationcore.Execer) error) {
	if uuid == "" {
		s.logger.Error("V2_DUAL_WRITE_FAILURE: could not resolve a uuid to mirror an upsert — reconcile manually",
			slog.String("code", MirrorFailure), slog.String("entity", entity), slog.String("op", "upsert"), slog.String("org", org))
		return
	}
	s.mirrorUpsert(entity, table, uuid, org, up)
}

// runTx opens a v2 transaction, bounds every statement in it at the DB level (Execer has no
// context, so write_timeout is enforced via SET LOCAL statement_timeout), runs fn, and
// commits — so one entity's full v2 footprint (artifact + type row + child rows) is atomic.
func (s *Sink) runTx(fn func(migrationcore.Execer) error) error {
	tx, err := s.v2.Begin()
	if err != nil {
		return fmt.Errorf("begin v2 tx: %w", err)
	}
	if s.v2Postgres {
		if _, err := tx.Exec(fmt.Sprintf("SET LOCAL statement_timeout = %d", s.writeTimeout.Milliseconds())); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("set v2 statement_timeout: %w", err)
		}
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit v2 tx: %w", err)
	}
	return nil
}

// failureRecord is one JSONL line in the dual-write failure log. Its fields double as the
// reconcile work list: `dbmigrate migrate -only-keys <this file>` reads op/table/key directly.
type failureRecord struct {
	Code       string `json:"code"`
	Entity     string `json:"entity"`
	Op         string `json:"op"`
	Table      string `json:"table"`
	Key        string `json:"key"`
	Org        string `json:"org,omitempty"`
	Error      string `json:"error,omitempty"`
	OccurredAt string `json:"occurred_at"`
}

// recordFailure logs at ERROR with the MirrorFailure marker AND appends a durable JSONL line
// to the failure log. Both carry the same marker so one string spans logs → file → reconcile.
// The failure log is a FILE, never the v1 DB — so the mirror never writes to the v1 database
// (v1 stays pristine; the only v1 access is the read-back, which is read-only). Recording is
// best-effort: if the append fails we have already logged with the marker.
func (s *Sink) recordFailure(entity, op, table, key, org string, cause error) {
	s.logger.Error("V2_DUAL_WRITE_FAILURE: v2 mirror write failed (v1 result unaffected)",
		slog.String("code", MirrorFailure),
		slog.String("entity", entity),
		slog.String("op", op),
		slog.String("table", table),
		slog.String("uuid", key),
		slog.String("org", org),
		slog.Any("err", cause),
	)
	line, err := json.Marshal(failureRecord{
		Code: MirrorFailure, Entity: entity, Op: op, Table: table, Key: key, Org: org,
		Error: cause.Error(), OccurredAt: time.Now().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		s.logger.Error("V2_DUAL_WRITE_FAILURE: could not marshal a failure record",
			slog.String("code", MirrorFailure), slog.Any("err", err))
		return
	}
	s.appendFailureLine(line)
}

// appendFailureLine appends one JSONL line to the failure log. Failures are rare (v2 is
// healthy in steady state), so it opens/appends/closes per line — each line is durable
// immediately with no long-lived handle to manage. failMu serializes concurrent requests
// within this process (across replicas each writes its own file — collect all for reconcile).
func (s *Sink) appendFailureLine(line []byte) {
	s.failMu.Lock()
	defer s.failMu.Unlock()
	f, err := os.OpenFile(s.failureLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		s.logger.Error("V2_DUAL_WRITE_FAILURE: could not open the failure log",
			slog.String("code", MirrorFailure), slog.String("path", s.failureLog), slog.Any("err", err))
		return
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		s.logger.Error("V2_DUAL_WRITE_FAILURE: could not append to the failure log",
			slog.String("code", MirrorFailure), slog.String("path", s.failureLog), slog.Any("err", err))
	}
}

// reporter is the migrationcore.Reporter for the live path: informational transform events
// (flags / dropped fields) are logged at DEBUG; a quarantine (an unparseable config blob,
// which makes UpsertX return ErrBlobUnparseable and thus surfaces as a mirror failure via
// mirror()) is logged at WARN with the marker. The durable failure line is written by
// mirror() on the op error, so the reporter never touches the failure log (no double count).
type reporter struct{ logger *slog.Logger }

func (r reporter) Flag(table, key, code string, oldV, newV any) {
	r.logger.Debug("v2 mirror flag", "table", table, "key", key, "code", code)
}

func (r reporter) Quarantine(table, key, code, detail string, row any) {
	r.logger.Warn("V2_DUAL_WRITE_FAILURE: v2 mirror quarantine (row cannot be mirrored)",
		slog.String("code", MirrorFailure), "table", table, "key", key, "reason", code, "detail", detail)
}

func (r reporter) Dropped(scope, table, key, feature, label string) {
	r.logger.Debug("v2 mirror dropped", "scope", scope, "table", table, "key", key, "feature", feature)
}

func (r reporter) DroppedFields(structName string, fields []string) {
	if len(fields) > 0 {
		r.logger.Debug("v2 mirror dropped unknown config fields", "struct", structName, "fields", fields)
	}
}
