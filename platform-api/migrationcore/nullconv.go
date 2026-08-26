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
	"time"
)

// Null-column conversions shared by every caller that scans v1 rows — the batch
// backfill (cmd/dbmigrate) and the live dual-write path (v1 repository/dualwrite).
// Keeping the single implementation here is the whole point: a fix to how a
// nullable v1 column becomes an insert arg lands in both paths at once.

// NullStrPtr converts a scanned nullable string into the *string form the V1 row
// structs use (nil when the column was NULL).
func NullStrPtr(ns sql.NullString) *string {
	if ns.Valid {
		s := ns.String
		return &s
	}
	return nil
}

// NullTimePtr converts a scanned nullable timestamp into a *time.Time (nil when
// the column was NULL).
func NullTimePtr(nt sql.NullTime) *time.Time {
	if nt.Valid {
		t := nt.Time
		return &t
	}
	return nil
}

// NullStr returns a scanned nullable string's value, or "" when it was NULL —
// used for the raw created_by actor the core's audit resolution consumes.
func NullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// NullBoolPtr converts a scanned nullable bool into a *bool (nil when NULL).
func NullBoolPtr(nb sql.NullBool) *bool {
	if nb.Valid {
		b := nb.Bool
		return &b
	}
	return nil
}

// NullInt64Ptr converts a scanned nullable int into a *int64 (nil when NULL).
func NullInt64Ptr(ni sql.NullInt64) *int64 {
	if ni.Valid {
		n := ni.Int64
		return &n
	}
	return nil
}
