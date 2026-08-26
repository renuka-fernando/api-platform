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

-- Enumerate carried handles that exceed the v2-native 40-char cap. Run this
-- against the v2 DB when 02-shrink-handle-to-40.sql FAILS, to see exactly which
-- artifacts block the shrink (also flagged HANDLE_EXCEEDS_NATIVE_CAP per row in
-- flags-<run-id>.jsonl). Zero rows ⇒ the shrink gate will pass.

SELECT 'organizations'          AS table_name, uuid, handle, length(handle) AS len FROM organizations          WHERE length(handle) > 40
UNION ALL SELECT 'applications',           uuid, handle, length(handle) FROM applications           WHERE length(handle) > 40
UNION ALL SELECT 'rest_apis',              uuid, handle, length(handle) FROM rest_apis              WHERE length(handle) > 40
UNION ALL SELECT 'llm_provider_templates', uuid, handle, length(handle) FROM llm_provider_templates WHERE length(handle) > 40
UNION ALL SELECT 'llm_providers',          uuid, handle, length(handle) FROM llm_providers          WHERE length(handle) > 40
UNION ALL SELECT 'llm_proxies',            uuid, handle, length(handle) FROM llm_proxies            WHERE length(handle) > 40
UNION ALL SELECT 'mcp_proxies',            uuid, handle, length(handle) FROM mcp_proxies            WHERE length(handle) > 40
UNION ALL SELECT 'websub_apis',            uuid, handle, length(handle) FROM websub_apis            WHERE length(handle) > 40
UNION ALL SELECT 'webbroker_apis',         uuid, handle, length(handle) FROM webbroker_apis         WHERE length(handle) > 40
ORDER BY len DESC, table_name;
