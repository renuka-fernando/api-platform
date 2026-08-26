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

-- Migration-window DDL — the CONFORMANCE GATE.  Apply AFTER `dbmigrate verify`
-- passes (RUNBOOK.md step 8), and — when the live dual-write client is in use —
-- only at/after cutover (keep VARCHAR(255) for the whole dual-write window).
-- Restores the columns widened by 01-widen-handle-to-255.sql back to the v2-native
-- VARCHAR(40).
--
-- This ALTER is the gate: PostgreSQL FAILS it loudly if ANY handle exceeds 40 —
--   ERROR: value too long for type character varying(40)
-- Run it inside a transaction so a failure rolls back cleanly (all-or-nothing).
--
--   PASS  -> every carried handle fits 40; the DB is byte-consistent with a
--            natively-created v2.
--   FAIL  -> at least one handle > 40 exists.  Enumerate with
--            handle-over-40-report.sql (also flagged HANDLE_EXCEEDS_NATIVE_CAP in
--            flags-<run-id>.jsonl), then either KEEP the column wide (v2 tolerates
--            >40 handles at read time — GET-by-handle has no length check) or RENAME
--            the offending artifacts in v1 and re-run migrate.  Do NOT truncate
--            in-place — that breaks the handle-based external references this
--            preservation protects.

BEGIN;

-- v2 core:
ALTER TABLE organizations          ALTER COLUMN handle TYPE VARCHAR(40);
ALTER TABLE applications           ALTER COLUMN handle TYPE VARCHAR(40);
ALTER TABLE rest_apis              ALTER COLUMN handle TYPE VARCHAR(40);
ALTER TABLE llm_provider_templates ALTER COLUMN handle TYPE VARCHAR(40);
ALTER TABLE llm_providers          ALTER COLUMN handle TYPE VARCHAR(40);
ALTER TABLE llm_proxies            ALTER COLUMN handle TYPE VARCHAR(40);
ALTER TABLE mcp_proxies            ALTER COLUMN handle TYPE VARCHAR(40);

-- EventGateway plugin (apply only if the plugin schema is installed):
ALTER TABLE websub_apis            ALTER COLUMN handle TYPE VARCHAR(40);
ALTER TABLE webbroker_apis         ALTER COLUMN handle TYPE VARCHAR(40);

COMMIT;
