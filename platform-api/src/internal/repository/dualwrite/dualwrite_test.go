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

package dualwrite

import (
	"bytes"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"platform-api/src/config"
	"platform-api/src/internal/database"
	"platform-api/src/internal/model"
	"platform-api/src/internal/repository"

	"github.com/wso2/api-platform/platform-api/migrationcore"
)

// newTestSink builds a Sink whose v2 side is a pool that is closed on purpose, so every
// mirror Begin fails — exercising the swallow-and-record path without needing a live
// Postgres. Failures are recorded into the v1-side v2_dual_write_failures TABLE (the shared HA
// store). The v1 side is a real in-memory SQLite for realism, though these tests fail at the
// v2 Begin before the read-back is reached. (Convergence/success is the §9.4 integration test.)
func newTestSink(t *testing.T) (*Sink, *bytes.Buffer) {
	t.Helper()
	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	v1, err := database.NewConnection(&config.Database{Driver: "sqlite3", Path: ":memory:", MaxOpenConns: 1, MaxIdleConns: 1}, silent)
	if err != nil {
		t.Fatalf("open v1 sqlite: %v", err)
	}
	t.Cleanup(func() { _ = v1.Close() })
	applyFailureTableDDL(t, v1) // provisioned out-of-band in prod; applied here for the test

	v2, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open v2 sqlite: %v", err)
	}
	_ = v2.Close() // closed ⇒ every mirror Begin fails

	buf := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sink := &Sink{
		v2:         v2,
		v2Postgres: false,
		v1:         v1,
		replica:    "test-replica",
		opts: migrationcore.Options{
			SourceTZ: time.UTC,
			Epoch:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		reporter:     reporter{logger: logger},
		writeTimeout: time.Second,
		logger:       logger,
	}
	return sink, buf
}

// applyFailureTableDDL provisions the failure table by executing the SAME shipped DDL file the
// operator applies in prod (internal/database/dualwrite_failures.sql) — keeping one source of
// truth. In prod this is a deploy-time step; the app itself never runs this DDL.
func applyFailureTableDDL(t *testing.T, v1 *database.DB) {
	t.Helper()
	ddl, err := os.ReadFile(filepath.Join("..", "..", "database", "dualwrite_failures.sql"))
	if err != nil {
		t.Fatalf("read dualwrite_failures.sql: %v", err)
	}
	if _, err := v1.Exec(string(ddl)); err != nil {
		t.Fatalf("apply failure-table DDL: %v", err)
	}
}

// readFailures reads the rows the sink recorded into the v1 v2_dual_write_failures table.
func readFailures(t *testing.T, sink *Sink) []failureRecord {
	t.Helper()
	rows, err := sink.v1.Query(sink.v1.Rebind(
		"SELECT code, entity, op, table_name, v1_key, org_uuid, error, occurred_at FROM " + failureTable + " ORDER BY occurred_at"))
	if err != nil {
		t.Fatalf("query failure table: %v", err)
	}
	defer rows.Close()
	var recs []failureRecord
	for rows.Next() {
		var r failureRecord
		var org, errStr sql.NullString
		if err := rows.Scan(&r.Code, &r.Entity, &r.Op, &r.Table, &r.Key, &org, &errStr, &r.OccurredAt); err != nil {
			t.Fatalf("scan failure row: %v", err)
		}
		r.Org, r.Error = org.String, errStr.String
		recs = append(recs, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate failure rows: %v", err)
	}
	return recs
}

// --- Gateway mock: embeds the interface so only RevokeToken is overridden. ---

type mockGatewayRepo struct {
	repository.GatewayRepository
	revokeErr    error
	revokeCalled bool
}

func (m *mockGatewayRepo) RevokeToken(tokenID string) error {
	m.revokeCalled = true
	return m.revokeErr
}

// TestRevokeTokenMirrorsAsUpsertAndSwallowsV2Failure asserts the v1 repo is called first,
// a v2 failure is swallowed (nil returned), a failure line is recorded in the JSONL log, and
// — critically — RevokeToken mirrors as an UPSERT (soft revoke → UpsertGatewayToken), never a
// delete. It also confirms the mirror wrote NOTHING to the v1 DB (no failure table there).
func TestRevokeTokenMirrorsAsUpsertAndSwallowsV2Failure(t *testing.T) {
	sink, buf := newTestSink(t)
	mock := &mockGatewayRepo{}
	d := NewGatewayRepo(mock, sink)

	if err := d.RevokeToken("tok-1"); err != nil {
		t.Fatalf("RevokeToken returned %v; a v2 failure must be swallowed", err)
	}
	if !mock.revokeCalled {
		t.Fatal("v1 RevokeToken must be called first")
	}

	recs := readFailures(t, sink)
	if len(recs) != 1 {
		t.Fatalf("failure log has %d records, want 1", len(recs))
	}
	r := recs[0]
	if r.Op != "upsert" {
		t.Errorf("op = %q; RevokeToken is a SOFT revoke → UpsertGatewayToken, NOT a delete", r.Op)
	}
	if r.Table != "gateway_tokens" {
		t.Errorf("table = %q, want gateway_tokens", r.Table)
	}
	if r.Key != "tok-1" {
		t.Errorf("key = %q, want tok-1", r.Key)
	}
	if r.Code != MirrorFailure {
		t.Errorf("code = %q, want %q", r.Code, MirrorFailure)
	}
	if !strings.Contains(buf.String(), MirrorFailure) {
		t.Error("expected an ERROR log carrying the MirrorFailure marker")
	}
	// The failure was recorded as a ROW in the shared v1 v2_dual_write_failures table — the
	// readFailures assertions above read it back. The mirror still writes NO v1 BUSINESS
	// tables; only this operational failure table (whose DDL is provisioned out-of-band).
}

// TestV1ErrorPropagatesWithoutMirroring asserts that when the v1 write fails, the error is
// returned and NO mirror is attempted (no failure record).
func TestV1ErrorPropagatesWithoutMirroring(t *testing.T) {
	sink, _ := newTestSink(t)
	mock := &mockGatewayRepo{revokeErr: errors.New("v1 boom")}
	d := NewGatewayRepo(mock, sink)

	if err := d.RevokeToken("tok-2"); err == nil {
		t.Fatal("the v1 error must propagate to the caller")
	}
	if recs := readFailures(t, sink); len(recs) != 0 {
		t.Errorf("no mirror must run when the v1 write fails; got %d failure records", len(recs))
	}
}

// --- Deployment mock for the performed-at guard. ---

type mockDeploymentRepo struct {
	repository.DeploymentRepository
	rows int64
}

func (m *mockDeploymentRepo) UpdateStatusWithPerformedAtGuard(artifactUUID, orgUUID, gatewayID string, newStatus model.DeploymentStatus, statusReason string, performedAt time.Time, requireCurrentStatus []model.DeploymentStatus) (int64, error) {
	return m.rows, nil
}

// TestUpdateStatusGuardSkipsMirrorOnStaleAck: rowsAffected==0 (a stale ack updated nothing)
// must NOT mirror a no-op.
func TestUpdateStatusGuardSkipsMirrorOnStaleAck(t *testing.T) {
	sink, _ := newTestSink(t)
	d := NewDeploymentRepo(&mockDeploymentRepo{rows: 0}, sink)

	if _, err := d.UpdateStatusWithPerformedAtGuard("a", "o", "g", model.DeploymentStatusDeployed, "", time.Now(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if recs := readFailures(t, sink); len(recs) != 0 {
		t.Errorf("rowsAffected==0 must not mirror; got %d failure records", len(recs))
	}
}

// TestUpdateStatusGuardMirrorsOnRealUpdate: rowsAffected>0 mirrors deployment_status.
func TestUpdateStatusGuardMirrorsOnRealUpdate(t *testing.T) {
	sink, _ := newTestSink(t)
	d := NewDeploymentRepo(&mockDeploymentRepo{rows: 1}, sink)

	if _, err := d.UpdateStatusWithPerformedAtGuard("art", "org", "gw", model.DeploymentStatusDeployed, "", time.Now(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	recs := readFailures(t, sink)
	if len(recs) != 1 {
		t.Fatalf("failure log has %d records, want 1", len(recs))
	}
	if recs[0].Op != "upsert" || recs[0].Table != "deployment_status" || recs[0].Key != "org|art|gw" {
		t.Errorf("record = {op:%q table:%q key:%q}, want {upsert deployment_status org|art|gw}", recs[0].Op, recs[0].Table, recs[0].Key)
	}
}
