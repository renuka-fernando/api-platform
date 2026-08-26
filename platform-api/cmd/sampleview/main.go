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

// sampleview generates a v1<->v2 sample-data view for the platform-api v1->v2
// migration, covering EVERY table in both databases so the transformation is
// visible per entity. Output is markdown (default) or JSON (-format json).
//
//	go run ./cmd/sampleview \
//	  -v1-dsn postgres://postgres:admin@localhost:5432/dbv1?sslmode=disable \
//	  -v2-dsn postgres://postgres:admin@localhost:5433/dbv2?sslmode=disable \
//	  -out MIGRATION_SAMPLE_DATA_VIEW.md            # markdown (expanded records)
//	go run ./cmd/sampleview -format json -out sample.json
//
// Values are shown in FULL (never trimmed). bytea columns are decoded to UTF-8
// text when the bytes are text (e.g. the config/openapi/manifest blobs), else
// shown as \x hex; such columns are marked "(bytea)" in markdown and carry a
// bytea flag in JSON, so a reader knows the text is a decoded view of a bytea.
// Tables are grouped: present in BOTH DBs, only in v1 (dropped/renamed), only in
// v2 (new/derived). DSNs also fall back to the V1_DSN / V2_DSN env vars.
package main

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// notes annotates a table (by its name in whichever DB) with the transform it
// illustrates. Optional — tables without a note are still dumped.
var notes = map[string]string{
	"organizations":                 "v1 `name` -> v2 `display_name`; handle carried (re-slugged, <=40 chars); v2 adds `idp_organization_ref_uuid` (=uuid placeholder), `data_version`, `created_by` (=migration actor -- v1 has no created_by).",
	"projects":                      "v1 has no handle / no created_by. v2 `handle` generated from `name` (SYNTHESIZED); `name`->`display_name`; `created_by`=migration actor.",
	"applications":                  "handle carried; `name`->`display_name`; `created_by` preserved (resolved to a v2 audit UUID).",
	"artifacts":                     "v2 parent row for every artifact kind (rest_api/llm_*/mcp/websub/webbroker). v1 `kind`->v2 `type`; carries org.",
	"rest_apis":                     "`name`->`display_name`; config blob reshaped (bytea, shown decoded); v2 adds `origin`=control_plane, `data_version`; lifecycle carried.",
	"llm_provider_templates":        "handle carried; `name`->`display_name`; configuration passed through (bytea, shown decoded).",
	"llm_providers":                 "`name`->`display_name`; config reshaped; v1 `status` dropped.",
	"llm_proxies":                   "`name`->`display_name`; config reshaped; v1 `status` dropped.",
	"mcp_proxies":                   "`name`->`display_name`; config reshaped; v1 `status` dropped; nullable project_uuid.",
	"websub_apis":                   "plugin table; `name`->`display_name`; config reshaped; lifecycle carried.",
	"webbroker_apis":                "plugin table; `name`->`display_name`; config reshaped; lifecycle carried.",
	"subscription_plans":            "handle generated from `plan_name`; `plan_name`->`display_name`; `billing_plan` dropped; throttle -> subscription_plan_limits child.",
	"subscription_plan_limits":      "v2-derived: the v1 plan's throttle_limit_count/unit becomes a limits row.",
	"subscriptions":                 "raw encrypted token + hash carried verbatim; no handle; dedup on (artifact,hash) and (org,artifact,application).",
	"gateways":                      "handle generated from v1 `name`; v1's real `display_name` preserved; `vhost` -> gateway_endpoints child; is_active/is_critical carried.",
	"gateway_endpoints":             "v2-derived: the v1 gateway `vhost` becomes an endpoint `url` row.",
	"gateway_tokens":                "token_hash/salt carried; RevokeToken is a soft status update (mirrored as upsert, not delete).",
	"gateway_custom_policies":       "policy_definition carried (bytea, shown decoded); keyed by (org,name,version).",
	"gateway_custom_policy_usages":  "policy_uuid <-> api_uuid usage rows.",
	"deployments":                   "content/metadata carried (bytea, shown decoded); ordered by created_at so base_deployment predecessors exist first.",
	"deployment_status":             "current state per (artifact,org,gateway); v1 has NO performed_by -> v2 `performed_by` = the migration actor (DEFAULTED_NULL).",
	"api_keys":                      "handle generated from `name`; `name`->`display_name`; created_by preserved; masked_api_key/api_key_hashes verbatim.",
	"user_idp_references":           "v2-only: synthesized identity map (v1 created_by/performed_by actor strings -> deterministic v2 audit UUIDs).",
	"association_mappings":          "v1-only name: the `gateway` rows become v2 `artifact_gateway_mappings`; `dev_portal` rows are dropped.",
	"artifact_gateway_mappings":     "v2 name for the v1 `association_mappings` gateway rows.",
	"application_artifacts":         "v1-only name: becomes v2 `application_artifact_mappings`.",
	"application_artifact_mappings": "v2 name for v1 `application_artifacts`.",
	"application_api_keys":          "v1-only name: becomes v2 `application_api_key_mappings`.",
	"application_api_key_mappings":  "v2 name for v1 `application_api_keys`.",
	"devportals":                    "removed-in-v2 feature: enumerated + dropped, never migrated.",
	"publication_mappings":          "removed-in-v2 feature (API publication): dropped, never migrated.",
	"v2_dual_write_failures":        "harness table in db-v1: records live-mirror failures (code, op, table, key, org) for reconcile.",
}

// preferred is a readable ordering; any table not listed is appended alphabetically.
var preferred = []string{
	"organizations", "projects", "applications", "artifacts",
	"rest_apis", "llm_provider_templates", "llm_providers", "llm_proxies",
	"mcp_proxies", "websub_apis", "webbroker_apis",
	"subscription_plans", "subscription_plan_limits", "subscriptions",
	"gateways", "gateway_endpoints", "gateway_tokens",
	"gateway_custom_policies", "gateway_custom_policy_usages",
	"artifact_gateway_mappings", "deployments", "deployment_status",
	"api_keys", "application_artifact_mappings", "application_api_key_mappings",
	"user_idp_references",
}

type pane struct {
	Cols  []string `json:"cols"`
	Bytea []string `json:"bytea"` // subset of Cols that are bytea (shown decoded)
	Rows  [][]any  `json:"rows"`  // each cell: string (full, untrimmed) or nil (null)
}

func main() {
	v1dsn := flag.String("v1-dsn", envOr("V1_DSN", "postgres://postgres:admin@localhost:5432/dbv1?sslmode=disable"), "PostgreSQL DSN of the v1 source DB")
	v2dsn := flag.String("v2-dsn", envOr("V2_DSN", "postgres://postgres:admin@localhost:5433/dbv2?sslmode=disable"), "PostgreSQL DSN of the v2 migrated DB")
	out := flag.String("out", "MIGRATION_SAMPLE_DATA_VIEW.md", "output path (\"-\" for stdout)")
	limit := flag.Int("limit", 10, "rows sampled per table")
	format := flag.String("format", "md", "output format: md | json")
	flag.Parse()

	db1 := mustOpen(*v1dsn)
	defer db1.Close()
	db2 := mustOpen(*v2dsn)
	defer db2.Close()

	t1, t2 := listTables(db1), listTables(db2)
	set1, set2 := toSet(t1), toSet(t2)
	order := orderedUnion(t1, t2)
	stats := map[string]int{
		"v1": len(t1), "v2": len(t2),
		"shared": inter(set1, set2), "v1only": diff(set1, set2), "v2only": diff(set2, set1),
	}
	actor := migrationActor(db2)

	var body string
	switch *format {
	case "json":
		body = renderJSON(db1, db2, order, set1, set2, stats, actor, *limit)
	case "md":
		body = renderMD(db1, db2, order, set1, set2, stats, actor, *limit)
	default:
		die("unknown -format %q (want md|json)", *format)
	}

	if *out == "-" {
		fmt.Print(body)
		return
	}
	if err := os.WriteFile(*out, []byte(body), 0o644); err != nil {
		die("write %s: %v", *out, err)
	}
	fmt.Fprintf(os.Stderr, "wrote %s (%s; v1=%d v2=%d shared=%d v1only=%d v2only=%d; %d rows each)\n",
		*out, *format, stats["v1"], stats["v2"], stats["shared"], stats["v1only"], stats["v2only"], *limit)
}

func renderMD(db1, db2 *sql.DB, order []string, s1, s2 map[string]bool, stats map[string]int, actor string, limit int) string {
	var b strings.Builder
	fmt.Fprintf(&b, `# Migration Sample Data View — db-v1 → db-v2

A sample (%d rows per table) of the source dump after the full v1→v2 batch migration,
covering **every table** in both databases. Generated by `+"`cmd/sampleview`"+`. Values are
shown in **full**, one column per line (expanded record form) so long blobs stay readable;
only cells over 20 KB (e.g. some `+"`openapi_spec`"+`) are cut with a `+"`[truncated — … of N bytes]`"+`
marker. Columns marked **(bytea)** are stored as bytea and shown as decoded UTF-8 text —
e.g. v2 config/openapi/manifest/policy_definition (binary bytes appear as `+"`\\x`"+` hex).

- **db-v1:** %d tables · **db-v2:** %d tables · **shared:** %d · **v1-only:** %d · **v2-only:** %d

`, limit, stats["v1"], stats["v2"], stats["shared"], stats["v1only"], stats["v2only"])

	if actor != "" {
		fmt.Fprintf(&b, "**Migration actor** — `%s` (a `user_idp_references` row with `idp_id = \"migration\"`). "+
			"This is a synthesized v2 identity, NOT a real user: it fills `created_by`/`updated_by`/`performed_by` wherever the "+
			"v1 row had no actor to carry (e.g. organizations, projects, `deployment_status.performed_by`). Such rows are flagged "+
			"`DEFAULTED_NULL`. It's deterministic from the migration epoch, so it's identical across the batch and the live mirror.\n\n", actor)
	}

	emit := func(title string, names []string) {
		b.WriteString("# " + title + "\n\n")
		for _, name := range names {
			fmt.Fprintf(&b, "## %s\n\n", name)
			if n := notes[name]; n != "" {
				fmt.Fprintf(&b, "%s\n\n", n)
			}
			if s1[name] {
				b.WriteString("**db-v1:**\n```\n" + mdRecords(sampleRaw(db1, name, limit)) + "```\n\n")
			}
			if s2[name] {
				b.WriteString("**db-v2:**\n```\n" + mdRecords(sampleRaw(db2, name, limit)) + "```\n\n")
			}
		}
	}
	var both, only1, only2 []string
	for _, n := range order {
		switch {
		case s1[n] && s2[n]:
			both = append(both, n)
		case s1[n]:
			only1 = append(only1, n)
		default:
			only2 = append(only2, n)
		}
	}
	emit("Tables in BOTH DBs (v1 → v2 comparison)", both)
	emit("Tables only in db-v1 (dropped or renamed in v2)", only1)
	emit("Tables only in db-v2 (new or derived in v2)", only2)
	return b.String()
}

func renderJSON(db1, db2 *sql.DB, order []string, s1, s2 map[string]bool, stats map[string]int, actor string, limit int) string {
	type entry struct {
		InV1 bool  `json:"inV1"`
		InV2 bool  `json:"inV2"`
		V1   *pane `json:"v1,omitempty"`
		V2   *pane `json:"v2,omitempty"`
	}
	tables := map[string]entry{}
	for _, name := range order {
		e := entry{InV1: s1[name], InV2: s2[name]}
		if s1[name] {
			p := sampleRaw(db1, name, limit)
			e.V1 = &p
		}
		if s2[name] {
			p := sampleRaw(db2, name, limit)
			e.V2 = &p
		}
		tables[name] = e
	}
	doc := map[string]any{
		"limit": limit, "stats": stats, "notes": notes, "order": order, "tables": tables,
		"migrationActor": actor,
	}
	out, err := json.Marshal(doc)
	if err != nil {
		die("marshal json: %v", err)
	}
	return string(out)
}

// sampleRaw returns cols, the bytea subset, and rows (cells: full string or nil).
func sampleRaw(db *sql.DB, table string, limit int) pane {
	order := "1"
	if hasUUID(db, table) {
		order = "uuid"
	}
	rows, err := db.Query(fmt.Sprintf(`SELECT * FROM "%s" ORDER BY %s LIMIT %d`, table, order, limit))
	if err != nil {
		return pane{Cols: []string{"error"}, Rows: [][]any{{err.Error()}}}
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return pane{Cols: []string{"error"}, Rows: [][]any{{err.Error()}}}
	}
	p := pane{Cols: cols, Bytea: byteaCols(db, table)}
	for rows.Next() {
		raw := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range raw {
			ptrs[i] = &raw[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return pane{Cols: []string{"error"}, Rows: [][]any{{err.Error()}}}
		}
		rec := make([]any, len(cols))
		for i, v := range raw {
			rec[i] = capCell(cell(v))
		}
		p.Rows = append(p.Rows, rec)
	}
	return p
}

// cellCap bounds a single cell so a pathological value (e.g. an ~817 KB
// openapi_spec) can't bloat the output. Normal values (config, manifest) are far
// under this and print in full. Larger cells are cut with a marker showing the
// true size — the only place any value is shortened.
const cellCap = 20000

// capCell truncates an over-cap string on a UTF-8 boundary and appends a marker.
func capCell(v any) any {
	s, ok := v.(string)
	if !ok || len(s) <= cellCap {
		return v
	}
	cut := cellCap
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + fmt.Sprintf("… [truncated — showing %d of %d bytes]", cut, len(s))
}

// cell converts a scanned driver value to a full display value: nil (null) or a
// string. bytea ([]byte) is decoded to UTF-8 text when valid, else \x hex.
// Newlines are preserved; length is bounded only by capCell.
func cell(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case []byte:
		if utf8.Valid(t) {
			return string(t)
		}
		return "\\x" + hex.EncodeToString(t)
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	case bool:
		if t {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprintf("%v", t)
	}
}

// mdRecords renders a pane in expanded "column : value" record form (psql \x
// style), which holds arbitrarily long, untrimmed values without breaking layout.
func mdRecords(p pane) string {
	if len(p.Rows) == 0 {
		return "(no rows)\n"
	}
	bset := toSet(p.Bytea)
	labels := make([]string, len(p.Cols))
	w := 0
	for i, c := range p.Cols {
		l := c
		if bset[c] {
			l = c + " (bytea)"
		}
		labels[i] = l
		if len(l) > w {
			w = len(l)
		}
	}
	indent := strings.Repeat(" ", w+3) // align wrapped/newline continuations under the value
	var b strings.Builder
	for ri, row := range p.Rows {
		bar := ri + 1
		fmt.Fprintf(&b, "-[ row %d ]%s\n", bar, strings.Repeat("-", max(3, w-len(fmt.Sprintf("row %d ", bar)))))
		for i, c := range row {
			val := "null"
			if c != nil {
				val = strings.ReplaceAll(c.(string), "\n", "\n"+indent)
			}
			fmt.Fprintf(&b, "%s : %s\n", pad(labels[i], w), val)
		}
	}
	return b.String()
}

func listTables(db *sql.DB) []string {
	rows, err := db.Query(`SELECT table_name FROM information_schema.tables
		WHERE table_schema='public' AND table_type='BASE TABLE' ORDER BY table_name`)
	if err != nil {
		die("list tables: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			die("scan table: %v", err)
		}
		out = append(out, t)
	}
	return out
}

// migrationActor returns the synthesized migration-actor uuid (the identity used
// for created_by/updated_by/performed_by where v1 had no actor), or "" if absent.
func migrationActor(db *sql.DB) string {
	var u string
	_ = db.QueryRow(`SELECT uuid FROM user_idp_references WHERE idp_id='migration' LIMIT 1`).Scan(&u)
	return u
}

func hasUUID(db *sql.DB, table string) bool {
	var n int
	_ = db.QueryRow(`SELECT count(*) FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 AND column_name='uuid'`, table).Scan(&n)
	return n > 0
}

// byteaCols returns the bytea columns of a table, in ordinal order.
func byteaCols(db *sql.DB, table string) []string {
	rows, err := db.Query(`SELECT column_name FROM information_schema.columns
		WHERE table_schema='public' AND table_name=$1 AND data_type='bytea' ORDER BY ordinal_position`, table)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out
}

func orderedUnion(a, b []string) []string {
	seen, all := map[string]bool{}, map[string]bool{}
	for _, t := range a {
		all[t] = true
	}
	for _, t := range b {
		all[t] = true
	}
	var out []string
	for _, t := range preferred {
		if all[t] && !seen[t] {
			out = append(out, t)
			seen[t] = true
		}
	}
	var rest []string
	for t := range all {
		if !seen[t] {
			rest = append(rest, t)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func toSet(s []string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

func inter(a, b map[string]bool) int {
	n := 0
	for k := range a {
		if b[k] {
			n++
		}
	}
	return n
}

func diff(a, b map[string]bool) int {
	n := 0
	for k := range a {
		if !b[k] {
			n++
		}
	}
	return n
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func mustOpen(dsn string) *sql.DB {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		die("open %s: %v", dsn, err)
	}
	if err := db.Ping(); err != nil {
		die("ping %s: %v", dsn, err)
	}
	return db
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "sampleview: "+format+"\n", a...)
	os.Exit(1)
}
