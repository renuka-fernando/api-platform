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

-- Migration-window DDL — apply AFTER the v2 core + EventGateway plugin schema and
-- BEFORE the backfill (RUNBOOK.md "Handle width").  Applied out-of-band; NOT part
-- of the canonical schema.postgres.sql (which stays v2-native VARCHAR(40)).
--
-- Why: the migrator (and the live dual-write client) preserve CARRIED handles
-- VERBATIM so handle-based external references stay stable — both v1 and v2 resolve
-- GET /…/{id} by handle.  v1 handles are valid slugs up to 63 chars, which do not
-- fit v2's native VARCHAR(40).  Widening these columns to VARCHAR(255) (matching v1)
-- lets any v1 handle land unchanged during the migration / dual-write window.
-- Reverted by 02-shrink-handle-to-40.sql (the conformance gate) at cutover.
--
-- Widening is metadata-only in PostgreSQL (instant, no table rewrite; UNIQUE(org,
-- handle) indexes are unaffected).  Only the CARRIED-handle tables are widened; the
-- generated-handle tables (projects, subscription_plans, gateways, api_keys) keep
-- VARCHAR(40) because their handles are derived from names and are always <= 40.

-- v2 core:
ALTER TABLE organizations          ALTER COLUMN handle TYPE VARCHAR(255);
ALTER TABLE applications           ALTER COLUMN handle TYPE VARCHAR(255);
ALTER TABLE rest_apis              ALTER COLUMN handle TYPE VARCHAR(255);
ALTER TABLE llm_provider_templates ALTER COLUMN handle TYPE VARCHAR(255);
ALTER TABLE llm_providers          ALTER COLUMN handle TYPE VARCHAR(255);
ALTER TABLE llm_proxies            ALTER COLUMN handle TYPE VARCHAR(255);
ALTER TABLE mcp_proxies            ALTER COLUMN handle TYPE VARCHAR(255);

-- EventGateway plugin (apply only if the plugin schema is installed):
ALTER TABLE websub_apis            ALTER COLUMN handle TYPE VARCHAR(255);
ALTER TABLE webbroker_apis         ALTER COLUMN handle TYPE VARCHAR(255);
