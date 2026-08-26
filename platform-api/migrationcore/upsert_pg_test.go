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

package migrationcore

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/wso2/api-platform/platform-api/internal/constants"
)

// These integration tests exercise the affordances the batch backfill never uses
// but the live dual-write path needs: ON CONFLICT DO UPDATE (upsert), InsertOnly
// (DO NOTHING), per-entity delete, and on-demand identity resolution. They require
// a PostgreSQL DB with the v2 core schema applied; set MIGRATIONCORE_TEST_DSN
// (e.g. postgres://postgres:admin@localhost:5433/dbtest?sslmode=disable).
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("MIGRATIONCORE_TEST_DSN")
	if dsn == "" {
		t.Skip("MIGRATIONCORE_TEST_DSN not set; skipping migrationcore Postgres integration tests")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("ping: %v", err)
	}
	return db
}

var testEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func baseOpts() Options {
	return Options{SourceTZ: time.UTC, Epoch: testEpoch, SkipIdentityUpsert: true}
}

func TestUpsertUpdateVsInsertOnly(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const uuid = "test-org-affordance-0001"
	_, _ = db.Exec("DELETE FROM organizations WHERE uuid = $1", uuid)

	getName := func() string {
		var n string
		if err := db.QueryRow("SELECT display_name FROM organizations WHERE uuid = $1", uuid).Scan(&n); err != nil {
			t.Fatalf("select: %v", err)
		}
		return n
	}

	const handle = "affordance-org"
	row := OrganizationV1Row{UUID: uuid, Name: "Acme", Region: "us"}

	// (1) upsert INSERT.
	if err := UpsertOrganizationV1(db, handle, row, baseOpts(), NopReporter{}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if got := getName(); got != "Acme" {
		t.Fatalf("after insert display_name=%q want Acme", got)
	}

	// (2) upsert UPDATE (InsertOnly=false → ON CONFLICT DO UPDATE).
	row.Name = "AcmeUpdated"
	opts := baseOpts()
	opts.InsertOnly = false
	if err := UpsertOrganizationV1(db, handle, row, opts, NopReporter{}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if got := getName(); got != "AcmeUpdated" {
		t.Fatalf("after upsert-update display_name=%q want AcmeUpdated", got)
	}

	// (3) InsertOnly=true → DO NOTHING (no change).
	row.Name = "ShouldNotStick"
	opts.InsertOnly = true
	if err := UpsertOrganizationV1(db, handle, row, opts, NopReporter{}); err != nil {
		t.Fatalf("insert-only: %v", err)
	}
	if got := getName(); got != "AcmeUpdated" {
		t.Fatalf("after InsertOnly display_name=%q want AcmeUpdated (unchanged)", got)
	}

	// (4) delete.
	if err := DeleteOrganization(db, baseOpts(), uuid); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var cnt int
	if err := db.QueryRow("SELECT count(*) FROM organizations WHERE uuid = $1", uuid).Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("after delete count=%d want 0", cnt)
	}
}

func TestResolveIdentityIncremental(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const idp = "affordance-user@example.com"
	_, _ = db.Exec("DELETE FROM user_idp_references WHERE idp_id = $1", idp)

	opts := baseOpts()
	opts.SkipIdentityUpsert = false // live path: upsert on demand

	uuid1, defaulted, err := ResolveIdentity(db, idp, opts)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if defaulted {
		t.Fatalf("non-empty actor should not be defaulted")
	}
	if want := DeterministicUUID(idp, testEpoch); uuid1 != want {
		t.Fatalf("uuid=%s want deterministic %s", uuid1, want)
	}
	// The row must now exist.
	var stored string
	if err := db.QueryRow("SELECT uuid FROM user_idp_references WHERE idp_id = $1", idp).Scan(&stored); err != nil {
		t.Fatalf("select idp: %v", err)
	}
	if stored != uuid1 {
		t.Fatalf("stored uuid=%s want %s", stored, uuid1)
	}
	// Idempotent: a second resolve neither errors nor duplicates.
	uuid2, _, err := ResolveIdentity(db, idp, opts)
	if err != nil || uuid2 != uuid1 {
		t.Fatalf("second resolve uuid=%s err=%v want %s", uuid2, err, uuid1)
	}
	var cnt int
	if err := db.QueryRow("SELECT count(*) FROM user_idp_references WHERE idp_id = $1", idp).Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("idp rows=%d want 1", cnt)
	}

	// Empty actor → the migration actor, flagged defaulted.
	_, def, err := ResolveIdentity(db, "", opts)
	if err != nil || !def {
		t.Fatalf("empty actor should default: def=%v err=%v", def, err)
	}
	_, _ = db.Exec("DELETE FROM user_idp_references WHERE idp_id IN ($1, $2)", idp, MigrationActorIDPID)
}

// TestDryRunNoWrite proves DryRun runs the transform but writes nothing.
func TestDryRunNoWrite(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const uuid = "test-org-dryrun-0002"
	_, _ = db.Exec("DELETE FROM organizations WHERE uuid = $1", uuid)

	opts := baseOpts()
	opts.DryRun = true
	if err := UpsertOrganizationV1(db, "dry", OrganizationV1Row{UUID: uuid, Name: "Dry", Region: "us"}, opts, NopReporter{}); err != nil {
		t.Fatalf("dry-run upsert: %v", err)
	}
	var cnt int
	if err := db.QueryRow("SELECT count(*) FROM organizations WHERE uuid = $1", uuid).Scan(&cnt); err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 0 {
		t.Fatalf("dry-run wrote a row (count=%d); want 0", cnt)
	}
}

// TestUpsertPreservesCreationAuditOnUpdate proves §8.2: a live ON CONFLICT DO UPDATE
// leaves created_at/created_by untouched (creation audit is immutable) while payload
// and updated_at/updated_by move to the updater's values.
func TestUpsertPreservesCreationAuditOnUpdate(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const org = "test-org-audit-8002"
	const proj = "test-proj-audit-8002"
	const app = "test-app-audit-8002"
	_, _ = db.Exec("DELETE FROM applications WHERE uuid = $1", app)
	_, _ = db.Exec("DELETE FROM projects WHERE uuid = $1", proj)
	_, _ = db.Exec("DELETE FROM organizations WHERE uuid = $1", org)
	defer func() {
		db.Exec("DELETE FROM applications WHERE uuid = $1", app)
		db.Exec("DELETE FROM projects WHERE uuid = $1", proj)
		db.Exec("DELETE FROM organizations WHERE uuid = $1", org)
	}()

	t1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)
	opts := baseOpts()
	opts.InsertOnly = false

	// organizations/projects have no created_by column, so application (which does) is
	// the vehicle for the §8.2 created-audit-immutability check. Seed the FK parents.
	if err := UpsertOrganizationV1(db, "audit-org-8002", OrganizationV1Row{UUID: org, Name: "AuditOrg", Region: "us"}, opts, NopReporter{}); err != nil {
		t.Fatalf("seed org: %v", err)
	}
	if err := UpsertProjectV1(db, "audit-proj-8002", ProjectV1Row{UUID: proj, Name: "AuditProj", Org: org}, opts, NopReporter{}); err != nil {
		t.Fatalf("seed project: %v", err)
	}

	// (1) create with actor "alice" at t1.
	row := ApplicationV1Row{UUID: app, ProjectUUID: proj, Org: org, Name: "Acme", Type: "standard",
		CreatedAt: sql.NullTime{Time: t1, Valid: true}, UpdatedAt: sql.NullTime{Time: t1, Valid: true},
		CreatedBy: sql.NullString{String: "alice", Valid: true}}
	if err := UpsertApplicationV1(db, "audit-app-8002", row, opts, NopReporter{}); err != nil {
		t.Fatalf("create: %v", err)
	}
	wantCreatedBy := DeterministicUUID("alice", testEpoch)

	// (2) update with actor "bob" at t2 — created_at/created_by must NOT move (§8.2).
	row.Name = "AcmeUpdated"
	row.CreatedAt = sql.NullTime{Time: t2, Valid: true}
	row.UpdatedAt = sql.NullTime{Time: t2, Valid: true}
	row.CreatedBy = sql.NullString{String: "bob", Valid: true}
	if err := UpsertApplicationV1(db, "audit-app-8002", row, opts, NopReporter{}); err != nil {
		t.Fatalf("update: %v", err)
	}

	var displayName, createdBy, updatedBy string
	var createdAt, updatedAt time.Time
	if err := db.QueryRow(
		"SELECT display_name, created_by, created_at, updated_by, updated_at FROM applications WHERE uuid = $1", app,
	).Scan(&displayName, &createdBy, &createdAt, &updatedBy, &updatedAt); err != nil {
		t.Fatalf("select: %v", err)
	}
	if displayName != "AcmeUpdated" {
		t.Errorf("display_name=%q want AcmeUpdated (payload should update)", displayName)
	}
	if createdBy != wantCreatedBy {
		t.Errorf("created_by=%q want %q (creation audit must be immutable on update)", createdBy, wantCreatedBy)
	}
	if !createdAt.UTC().Equal(t1) {
		t.Errorf("created_at=%s want %s (creation audit must be immutable on update)", createdAt.UTC(), t1)
	}
	if want := DeterministicUUID("bob", testEpoch); updatedBy != want {
		t.Errorf("updated_by=%q want %q (updated audit should move to the updater)", updatedBy, want)
	}
	if !updatedAt.UTC().Equal(t2) {
		t.Errorf("updated_at=%s want %s (updated audit should move)", updatedAt.UTC(), t2)
	}
}

// TestUpsertGatewayReplacesEndpointOnVhostChange proves §8.3: a live UpsertGateway
// UPDATE with a changed vhost leaves exactly one gateway_endpoints row (the stale one
// is removed), rather than accumulating a second SERIAL-keyed row.
func TestUpsertGatewayReplacesEndpointOnVhostChange(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const org = "test-org-gw-8003"
	const gw = "test-gw-8003"
	_, _ = db.Exec("DELETE FROM gateways WHERE uuid = $1", gw)
	_, _ = db.Exec("DELETE FROM organizations WHERE uuid = $1", org)
	defer func() {
		db.Exec("DELETE FROM gateways WHERE uuid = $1", gw)
		db.Exec("DELETE FROM organizations WHERE uuid = $1", org)
	}()

	opts := baseOpts()
	opts.InsertOnly = false
	if err := UpsertOrganizationV1(db, "gw-org-8003", OrganizationV1Row{UUID: org, Name: "GwOrg", Region: "us"}, opts, NopReporter{}); err != nil {
		t.Fatalf("seed org: %v", err)
	}

	countEndpoints := func() int {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM gateway_endpoints WHERE gateway_uuid = $1", gw).Scan(&n); err != nil {
			t.Fatalf("count endpoints: %v", err)
		}
		return n
	}
	getEndpoint := func() string {
		var u string
		if err := db.QueryRow("SELECT url FROM gateway_endpoints WHERE gateway_uuid = $1", gw).Scan(&u); err != nil {
			t.Fatalf("get endpoint: %v", err)
		}
		return u
	}

	base := GatewayV1Row{UUID: gw, Org: org, DisplayName: "GW", Version: "1.0",
		FuncType: "regular", Vhost: "v1.example.com", Properties: []byte("{}"), IsActive: sql.NullBool{Bool: true, Valid: true}}
	if err := UpsertGatewayV1(db, "gw-8003", base, opts, NopReporter{}); err != nil {
		t.Fatalf("create gateway: %v", err)
	}
	if n := countEndpoints(); n != 1 || getEndpoint() != "v1.example.com" {
		t.Fatalf("after create: endpoints=%d url=%q want 1 / v1.example.com", n, getEndpoint())
	}

	// vhost change → the old endpoint must be replaced, not accumulated (§8.3).
	base.Vhost = "v2.example.com"
	if err := UpsertGatewayV1(db, "gw-8003", base, opts, NopReporter{}); err != nil {
		t.Fatalf("update gateway: %v", err)
	}
	if n := countEndpoints(); n != 1 || getEndpoint() != "v2.example.com" {
		t.Fatalf("after vhost change: endpoints=%d url=%q want 1 / v2.example.com (stale endpoint must be removed)", n, getEndpoint())
	}
}

// TestUpsertSubscriptionPlanReplacesLimitOnThrottleChange proves §8.3: a live
// UpsertSubscriptionPlan UPDATE that changes the throttle unit (which shifts the
// limit's natural key) leaves exactly one subscription_plan_limits row.
func TestUpsertSubscriptionPlanReplacesLimitOnThrottleChange(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	const org = "test-org-plan-8003"
	const plan = "test-plan-8003"
	_, _ = db.Exec("DELETE FROM subscription_plans WHERE uuid = $1", plan)
	_, _ = db.Exec("DELETE FROM organizations WHERE uuid = $1", org)
	defer func() {
		db.Exec("DELETE FROM subscription_plans WHERE uuid = $1", plan)
		db.Exec("DELETE FROM organizations WHERE uuid = $1", org)
	}()

	opts := baseOpts()
	opts.InsertOnly = false
	if err := UpsertOrganizationV1(db, "plan-org-8003", OrganizationV1Row{UUID: org, Name: "PlanOrg", Region: "us"}, opts, NopReporter{}); err != nil {
		t.Fatalf("seed org: %v", err)
	}

	countLimits := func() int {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM subscription_plan_limits WHERE subscription_plan_uuid = $1", plan).Scan(&n); err != nil {
			t.Fatalf("count limits: %v", err)
		}
		return n
	}
	getLimit := func() (string, int64) {
		var unit string
		var count int64
		if err := db.QueryRow("SELECT time_unit, limit_count FROM subscription_plan_limits WHERE subscription_plan_uuid = $1", plan).Scan(&unit, &count); err != nil {
			t.Fatalf("get limit: %v", err)
		}
		return unit, count
	}

	base := SubscriptionPlanV1Row{UUID: plan, PlanName: "Plan", Org: org, Status: "ACTIVE",
		ThrottleUnit: sql.NullString{String: "min", Valid: true}, ThrottleCount: sql.NullInt64{Int64: 100, Valid: true}}
	if err := UpsertSubscriptionPlanV1(db, "plan-8003", base, opts, NopReporter{}); err != nil {
		t.Fatalf("create plan: %v", err)
	}
	if n := countLimits(); n != 1 {
		t.Fatalf("after create: limits=%d want 1", n)
	}
	if u, c := getLimit(); u != constants.ThrottleLimitUnitMinute || c != 100 {
		t.Fatalf("after create: limit=%s/%d want %s/100", u, c, constants.ThrottleLimitUnitMinute)
	}

	// throttle unit change (min→hour, which moves the natural key) → the old MINUTE
	// limit must be replaced, not left behind (§8.3).
	base.ThrottleUnit = sql.NullString{String: "hour", Valid: true}
	base.ThrottleCount = sql.NullInt64{Int64: 200, Valid: true}
	if err := UpsertSubscriptionPlanV1(db, "plan-8003", base, opts, NopReporter{}); err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if n := countLimits(); n != 1 {
		t.Fatalf("after throttle change: limits=%d want 1 (stale MINUTE limit must be removed)", n)
	}
	if u, c := getLimit(); u != constants.ThrottleLimitUnitHour || c != 200 {
		t.Fatalf("after throttle change: limit=%s/%d want %s/200", u, c, constants.ThrottleLimitUnitHour)
	}
}
