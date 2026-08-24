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

// Command dbmigrate performs a one-time, offline migration of a Platform API v1
// PostgreSQL database into a Platform API v2 database whose schema (core +
// EventGateway plugin DDL) already exists — the tool never creates it. It always
// migrates all six artifact types. See MIGRATION_MAPPING.md for the full mapping
// and decisions, and RUNBOOK.md for the operational procedure.
//
// Usage:
//
//	dbmigrate migrate -v1-dsn <url> -v2-dsn <url> -out-dir <dir> -run-id <id> [-dry-run] [flags]
//	dbmigrate verify  -v1-dsn <url> -v2-dsn <url> -out-dir <dir> -run-id <id>
//
// The subscription-token encryption key is read from the APIP_MIGRATION_ENCRYPTION_KEY
// environment variable or from -encryption-key-file (never a CLI flag, which would leak
// via ps/shell history).
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const defaultMigrationEpoch = "2026-01-01T00:00:00Z"

// Options holds the parsed command-line configuration shared by the subcommands.
type Options struct {
	V1DSN   string
	V2DSN   string
	OutDir  string
	RunID   string
	DryRun  bool
	Verbose bool

	// Transform/decision knobs.
	SourceTZ                 string
	IDPRefStrategy           string
	GroupIDStrategy          string
	MigrationEpoch           time.Time
	PopulateArtifactSubPlans bool
	AuditMarker              bool
	SkipDecryptCheck         bool
	DecryptSampleSize        int

	// Targeted reconciliation (§8.1). OnlyKeys is a file of "<op> <table> <key>" lines
	// to replay through the shared UpsertX/DeleteX; Since (no key list) triggers a full
	// idempotent re-sync. Either one puts migrate into reconcile mode (InsertOnly:false).
	OnlyKeys string
	Since    string

	// Encryption-key source (env var preferred; file as an alternative). Never a
	// flag, which would leak the key via ps/shell history.
	EncKeyFile string

	// Secrets (not flags).
	EncryptionKey []byte // 32 bytes, or nil when unavailable
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	args := os.Args[2:]

	var err error
	switch sub {
	case "migrate":
		err = runMigrate(args)
	case "verify":
		err = runVerify(args)
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", sub)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "\ndbmigrate %s: %v\n", sub, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `dbmigrate — Platform API v1 -> v2 database migration

Subcommands:
  migrate   Transform v1 data and write it into a fresh v2 database.
  verify    Read-only completeness/correctness gate (non-zero exit on FAIL).

Run "dbmigrate migrate -h" or "dbmigrate verify -h" for flags.

The subscription-token key comes from APIP_MIGRATION_ENCRYPTION_KEY or
-encryption-key-file, never a CLI flag.
`)
}

// registerCommonFlags wires the flags shared by both subcommands into fs.
func registerCommonFlags(fs *flag.FlagSet, o *Options) {
	fs.StringVar(&o.V1DSN, "v1-dsn", "", "PostgreSQL DSN of the v1 source DB (required), e.g. postgres://user:pass@host:5432/db?sslmode=disable")
	fs.StringVar(&o.V2DSN, "v2-dsn", "", "PostgreSQL DSN of the v2 target DB (required)")
	fs.StringVar(&o.OutDir, "out-dir", "", "Directory for state/report/quarantine/flags files (required)")
	fs.StringVar(&o.RunID, "run-id", "", "Stable identifier for this run (required); names the state/report/quarantine/flags files and must match between the migrate run, its resume, and its verify")
	fs.BoolVar(&o.Verbose, "v", false, "Verbose (debug) logging")
}

// newLogger builds a slog logger honoring -v.
func newLogger(o *Options) *slog.Logger {
	level := slog.LevelInfo
	if o.Verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

// validateCommon checks the flags shared by both subcommands.
func validateCommon(o *Options) error {
	if o.V1DSN == "" || o.V2DSN == "" {
		return fmt.Errorf("-v1-dsn and -v2-dsn are required")
	}
	if o.OutDir == "" {
		return fmt.Errorf("-out-dir is required")
	}
	if o.RunID == "" {
		return fmt.Errorf("-run-id is required (must match between the migrate run, its resume, and its verify)")
	}
	// -source-tz is required with no default: naive v1 TIMESTAMP values carry no zone,
	// and time.LoadLocation("") would silently resolve to UTC, so an operator who never
	// determined the source zone would migrate every timestamp under an unverified guess.
	// Determine it first (see RUNBOOK.md) and pass the same value to migrate and verify.
	if o.SourceTZ == "" {
		return fmt.Errorf("-source-tz is required (determine the zone the v1 TIMESTAMP values were written in; see RUNBOOK.md) and must match between the migrate run and its verify")
	}
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return fmt.Errorf("create out-dir: %w", err)
	}
	return nil
}
