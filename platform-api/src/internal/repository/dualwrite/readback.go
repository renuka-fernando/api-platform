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

// Read-back (§6.2). Rather than reconstruct the migrationcore rows from the domain model —
// which would drift on every field the model does not carry faithfully (the encrypted +
// hashed subscription token, the bundled LLM-template config, the "{}" default for gateway
// properties, the manifest that is not on the Gateway model, the config blobs that are
// parsed structs) — each mirror reads the RAW v1 columns straight back from the v1 DB, using
// the SAME SELECTs the batch iterator uses (cmd/dbmigrate/migrate_tables.go) filtered to one
// primary key. That guarantees the live path feeds migrationcore byte-for-byte what a fresh
// batch of the final v1 state would, so the two converge (the §9.4 pass condition). Handles
// go through migrationcore.Slug to match the batch's carriedHandle / generate (no-collision
// case).

import (
	"database/sql"

	"platform-api/src/internal/database"

	"github.com/wso2/api-platform/platform-api/migrationcore"
)

// The read functions scan the RAW v1 columns straight into the migrationcore XV1Row
// structs (sql.Null* fields), so the v1→v2 mapping + null conversion live in ONE place
// (migrationcore). CARRIED-handle entities (organizations, applications, rest_apis, llm_*,
// mcp, websub, webbroker) return the v1 handle VERBATIM — preserved, matching the batch's
// carriedHandle; this keeps handle-based external references stable across the migration
// (both v1 and v2 resolve GET /…/{id} by handle). GENERATED-handle entities (projects,
// subscription_plans, gateways, api_keys) return migrationcore.Slug(name).

// ---- organizations ----

// readOrganizationRow reads the raw v1 organizations row and returns the v1 handle
// VERBATIM (carried — matches the batch's carriedHandle) alongside a faithful v1
// row. The name→display_name and null-conversion mapping now lives in
// migrationcore.UpsertOrganizationV1.
func readOrganizationRow(v1 *database.DB, uuid string) (string, migrationcore.OrganizationV1Row, error) {
	var handle, name, region string
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT handle, name, region, created_at, updated_at FROM organizations WHERE uuid = ?`), uuid).
		Scan(&handle, &name, &region, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.OrganizationV1Row{}, err
	}
	return handle, migrationcore.OrganizationV1Row{
		UUID: uuid, Name: name, Region: region,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// ---- projects (no v1 handle/created_by; handle generated from name) ----

func readProjectRow(v1 *database.DB, uuid string) (string, migrationcore.ProjectV1Row, error) {
	var name, org string
	var description sql.NullString
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT name, organization_uuid, description, created_at, updated_at FROM projects WHERE uuid = ?`), uuid).
		Scan(&name, &org, &description, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.ProjectV1Row{}, err
	}
	return migrationcore.Slug(name), migrationcore.ProjectV1Row{
		UUID: uuid, Name: name, Org: org, Description: description,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// ---- applications ----

func readApplicationRow(v1 *database.DB, uuid string) (string, migrationcore.ApplicationV1Row, error) {
	var handle, projectUUID, org, name, typ string
	var createdBy, description sql.NullString
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT handle, project_uuid, organization_uuid, created_by, name, description, type, created_at, updated_at
		 FROM applications WHERE uuid = ?`), uuid).
		Scan(&handle, &projectUUID, &org, &createdBy, &name, &description, &typ, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.ApplicationV1Row{}, err
	}
	return handle, migrationcore.ApplicationV1Row{
		UUID: uuid, ProjectUUID: projectUUID, Org: org, Name: name, Type: typ,
		Description: description, CreatedAt: createdAt, UpdatedAt: updatedAt, CreatedBy: createdBy,
	}, nil
}

// ---- rest_apis ----

func readRestAPIRow(v1 *database.DB, uuid string) (string, migrationcore.RestAPIV1Row, error) {
	var handle, name, version, org, projectUUID string
	var description, createdBy, lifecycle, transport sql.NullString
	var config []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT a.handle, a.name, a.version, a.organization_uuid, t.project_uuid,
		        t.description, t.created_by, t.lifecycle_status, t.transport, t.configuration,
		        a.created_at, a.updated_at
		 FROM rest_apis t INNER JOIN artifacts a ON t.uuid = a.uuid WHERE t.uuid = ?`), uuid).
		Scan(&handle, &name, &version, &org, &projectUUID, &description, &createdBy, &lifecycle, &transport, &config, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.RestAPIV1Row{}, err
	}
	return handle, migrationcore.RestAPIV1Row{
		UUID: uuid, Name: name, Version: version, Org: org, ProjectUUID: projectUUID,
		Description: description, Lifecycle: lifecycle, Transport: transport, Configuration: config,
		CreatedAt: createdAt, UpdatedAt: updatedAt, CreatedBy: createdBy,
	}, nil
}

// ---- llm_provider_templates ----

func readLLMTemplateRow(v1 *database.DB, uuid string) (string, migrationcore.LLMTemplateV1Row, error) {
	var org, handle, name string
	var description, createdBy sql.NullString
	var config []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT organization_uuid, handle, name, description, created_by, configuration, created_at, updated_at
		 FROM llm_provider_templates WHERE uuid = ?`), uuid).
		Scan(&org, &handle, &name, &description, &createdBy, &config, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.LLMTemplateV1Row{}, err
	}
	return handle, migrationcore.LLMTemplateV1Row{
		UUID: uuid, Org: org, Name: name, Description: description,
		Configuration: config, CreatedAt: createdAt, UpdatedAt: updatedAt, CreatedBy: createdBy,
	}, nil
}

// ---- llm_providers ----

func readLLMProviderRow(v1 *database.DB, uuid string) (string, migrationcore.LLMProviderV1Row, error) {
	var handle, name, version, org, templateUUID string
	var description, createdBy, openapiSpec, modelList, status sql.NullString
	var config []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT a.handle, a.name, a.version, a.organization_uuid, t.template_uuid,
		        t.description, t.created_by, t.openapi_spec, t.model_list, t.status, t.configuration,
		        a.created_at, a.updated_at
		 FROM llm_providers t INNER JOIN artifacts a ON t.uuid = a.uuid WHERE t.uuid = ?`), uuid).
		Scan(&handle, &name, &version, &org, &templateUUID, &description, &createdBy, &openapiSpec, &modelList, &status, &config, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.LLMProviderV1Row{}, err
	}
	return handle, migrationcore.LLMProviderV1Row{
		UUID: uuid, Name: name, Version: version, Org: org, TemplateUUID: templateUUID,
		Description: description, OpenAPISpec: openapiSpec, ModelList: modelList, Status: status,
		Configuration: config, CreatedAt: createdAt, UpdatedAt: updatedAt, CreatedBy: createdBy,
	}, nil
}

// ---- llm_proxies ----

func readLLMProxyRow(v1 *database.DB, uuid string) (string, migrationcore.LLMProxyV1Row, error) {
	var handle, name, version, org, projectUUID, providerUUID string
	var description, createdBy, openapiSpec, status sql.NullString
	var config []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT a.handle, a.name, a.version, a.organization_uuid, t.project_uuid, t.provider_uuid,
		        t.description, t.created_by, t.openapi_spec, t.status, t.configuration,
		        a.created_at, a.updated_at
		 FROM llm_proxies t INNER JOIN artifacts a ON t.uuid = a.uuid WHERE t.uuid = ?`), uuid).
		Scan(&handle, &name, &version, &org, &projectUUID, &providerUUID, &description, &createdBy, &openapiSpec, &status, &config, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.LLMProxyV1Row{}, err
	}
	return handle, migrationcore.LLMProxyV1Row{
		UUID: uuid, Name: name, Version: version, ProjectUUID: projectUUID, Org: org, ProviderUUID: providerUUID,
		Description: description, OpenAPISpec: openapiSpec, Status: status,
		Configuration: config, CreatedAt: createdAt, UpdatedAt: updatedAt, CreatedBy: createdBy,
	}, nil
}

// ---- mcp_proxies ----

func readMCPProxyRow(v1 *database.DB, uuid string) (string, migrationcore.MCPProxyV1Row, error) {
	var handle, name, version, org string
	var projectUUID, description, createdBy, status sql.NullString
	var config []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT a.handle, a.name, a.version, a.organization_uuid, t.project_uuid,
		        t.description, t.created_by, t.status, t.configuration, a.created_at, a.updated_at
		 FROM mcp_proxies t INNER JOIN artifacts a ON t.uuid = a.uuid WHERE t.uuid = ?`), uuid).
		Scan(&handle, &name, &version, &org, &projectUUID, &description, &createdBy, &status, &config, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.MCPProxyV1Row{}, err
	}
	return handle, migrationcore.MCPProxyV1Row{
		UUID: uuid, Name: name, Version: version, Org: org,
		ProjectUUID: projectUUID, Description: description, Status: status,
		Configuration: config, CreatedAt: createdAt, UpdatedAt: updatedAt, CreatedBy: createdBy,
	}, nil
}

// ---- websub_apis ----

func readWebSubRow(v1 *database.DB, uuid string) (string, migrationcore.WebSubV1Row, error) {
	var handle, name, version, org, projectUUID string
	var description, createdBy, lifecycle, transport sql.NullString
	var config []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT a.handle, a.name, a.version, a.organization_uuid, t.project_uuid,
		        t.description, t.created_by, t.lifecycle_status, t.transport, t.configuration,
		        a.created_at, a.updated_at
		 FROM websub_apis t INNER JOIN artifacts a ON t.uuid = a.uuid WHERE t.uuid = ?`), uuid).
		Scan(&handle, &name, &version, &org, &projectUUID, &description, &createdBy, &lifecycle, &transport, &config, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.WebSubV1Row{}, err
	}
	return handle, migrationcore.WebSubV1Row{
		UUID: uuid, Name: name, Version: version, Org: org, ProjectUUID: projectUUID,
		Description: description, Lifecycle: lifecycle, Transport: transport, Configuration: config,
		CreatedAt: createdAt, UpdatedAt: updatedAt, CreatedBy: createdBy,
	}, nil
}

// ---- webbroker_apis ----

func readWebBrokerRow(v1 *database.DB, uuid string) (string, migrationcore.WebBrokerV1Row, error) {
	var handle, name, version, org, projectUUID string
	var description, createdBy, lifecycle, transport sql.NullString
	var config []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT a.handle, a.name, a.version, a.organization_uuid, t.project_uuid,
		        t.description, t.created_by, t.lifecycle_status, t.transport, t.configuration,
		        a.created_at, a.updated_at
		 FROM webbroker_apis t INNER JOIN artifacts a ON t.uuid = a.uuid WHERE t.uuid = ?`), uuid).
		Scan(&handle, &name, &version, &org, &projectUUID, &description, &createdBy, &lifecycle, &transport, &config, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.WebBrokerV1Row{}, err
	}
	return handle, migrationcore.WebBrokerV1Row{
		UUID: uuid, Name: name, Version: version, Org: org, ProjectUUID: projectUUID,
		Description: description, Lifecycle: lifecycle, Transport: transport, Configuration: config,
		CreatedAt: createdAt, UpdatedAt: updatedAt, CreatedBy: createdBy,
	}, nil
}

// ---- subscription_plans (no v1 handle/created_by; handle generated from plan_name) ----

func readSubscriptionPlanRow(v1 *database.DB, uuid string) (string, migrationcore.SubscriptionPlanV1Row, error) {
	var planName, org, status string
	var billingPlan, throttleUnit sql.NullString
	var stopOnQuota sql.NullBool
	var throttleCount sql.NullInt64
	var expiry, createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT plan_name, billing_plan, stop_on_quota_reach, throttle_limit_count, throttle_limit_unit,
		        expiry_time, organization_uuid, status, created_at, updated_at
		 FROM subscription_plans WHERE uuid = ?`), uuid).
		Scan(&planName, &billingPlan, &stopOnQuota, &throttleCount, &throttleUnit, &expiry, &org, &status, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.SubscriptionPlanV1Row{}, err
	}
	return migrationcore.Slug(planName), migrationcore.SubscriptionPlanV1Row{
		UUID: uuid, PlanName: planName, Org: org, Status: status,
		BillingPlan: billingPlan, ThrottleUnit: throttleUnit,
		StopOnQuota: stopOnQuota, ThrottleCount: throttleCount,
		ExpiryTime: expiry, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// ---- subscriptions (token/hash are read raw: the model carries only the DECRYPTED token) ----

func readSubscriptionRow(v1 *database.DB, uuid string) (migrationcore.SubscriptionV1Row, error) {
	var apiUUID, subscriberID, token, hash, org, status string
	var applicationID, planUUID sql.NullString
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT api_uuid, subscriber_id, application_id, subscription_token, subscription_token_hash,
		        subscription_plan_uuid, organization_uuid, status, created_at, updated_at
		 FROM subscriptions WHERE uuid = ?`), uuid).
		Scan(&apiUUID, &subscriberID, &applicationID, &token, &hash, &planUUID, &org, &status, &createdAt, &updatedAt)
	if err != nil {
		return migrationcore.SubscriptionV1Row{}, err
	}
	return migrationcore.SubscriptionV1Row{
		UUID: uuid, ArtifactUUID: apiUUID, SubscriberID: subscriberID, Token: token, Hash: hash, Org: org, Status: status,
		ApplicationID: applicationID, PlanUUID: planUUID, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// ---- gateways (properties + manifest are read raw; handle generated from name) ----

func readGatewayRow(v1 *database.DB, uuid string) (string, migrationcore.GatewayV1Row, error) {
	var org, name, version, displayName, funcType, vhost string
	var description sql.NullString
	var properties, manifest []byte
	var isCritical, isActive sql.NullBool
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT organization_uuid, name, version, display_name, description, properties, vhost,
		        is_critical, gateway_functionality_type, is_active, manifest, created_at, updated_at
		 FROM gateways WHERE uuid = ?`), uuid).
		Scan(&org, &name, &version, &displayName, &description, &properties, &vhost, &isCritical, &funcType, &isActive, &manifest, &createdAt, &updatedAt)
	if err != nil {
		return "", migrationcore.GatewayV1Row{}, err
	}
	return migrationcore.Slug(name), migrationcore.GatewayV1Row{
		UUID: uuid, Org: org, DisplayName: displayName, Version: version,
		FuncType: funcType, Vhost: vhost, Description: description, Properties: properties, Manifest: manifest,
		IsCritical: isCritical, IsActive: isActive, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// ---- gateway_tokens ----

func readGatewayTokenRow(v1 *database.DB, uuid string) (migrationcore.GatewayTokenV1Row, error) {
	var gatewayUUID, tokenHash, salt, status string
	var createdAt, revokedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT gateway_uuid, token_hash, salt, status, created_at, revoked_at FROM gateway_tokens WHERE uuid = ?`), uuid).
		Scan(&gatewayUUID, &tokenHash, &salt, &status, &createdAt, &revokedAt)
	if err != nil {
		return migrationcore.GatewayTokenV1Row{}, err
	}
	return migrationcore.GatewayTokenV1Row{
		UUID: uuid, GatewayUUID: gatewayUUID, TokenHash: tokenHash, Salt: salt, Status: status,
		CreatedAt: createdAt, RevokedAt: revokedAt,
	}, nil
}

// ---- gateway_custom_policies ----

func readGatewayCustomPolicyRow(v1 *database.DB, uuid string) (migrationcore.GatewayCustomPolicyV1Row, error) {
	var org, name, version string
	var displayName, description sql.NullString
	var policyDef []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT organization_uuid, name, display_name, version, description, policy_definition, created_at, updated_at
		 FROM gateway_custom_policies WHERE uuid = ?`), uuid).
		Scan(&org, &name, &displayName, &version, &description, &policyDef, &createdAt, &updatedAt)
	if err != nil {
		return migrationcore.GatewayCustomPolicyV1Row{}, err
	}
	return migrationcore.GatewayCustomPolicyV1Row{
		UUID: uuid, Org: org, Name: name, Version: version, DisplayName: displayName, Description: description,
		PolicyDefinition: policyDef, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// ---- deployments ----

func readDeploymentRow(v1 *database.DB, deploymentID string) (migrationcore.DeploymentV1Row, error) {
	var name, artifactUUID, org, gatewayUUID string
	var baseDeployment, metadata sql.NullString
	var content []byte
	var createdAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT name, artifact_uuid, organization_uuid, gateway_uuid, base_deployment_id, content, metadata, created_at
		 FROM deployments WHERE deployment_id = ?`), deploymentID).
		Scan(&name, &artifactUUID, &org, &gatewayUUID, &baseDeployment, &content, &metadata, &createdAt)
	if err != nil {
		return migrationcore.DeploymentV1Row{}, err
	}
	return migrationcore.DeploymentV1Row{
		UUID: deploymentID, Name: name, ArtifactUUID: artifactUUID, Org: org, GatewayUUID: gatewayUUID,
		BaseDeploymentUUID: migrationcore.NullStrPtr(baseDeployment), Metadata: metadata, Content: content, CreatedAt: createdAt,
	}, nil
}

// ---- deployment_status (current state per artifact+org+gateway) ----

func readDeploymentStatusRow(v1 *database.DB, artifactUUID, org, gatewayUUID string) (migrationcore.DeploymentStatusV1Row, error) {
	var deploymentUUID, status string
	var statusDesired, statusReason sql.NullString
	var performedAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT deployment_id, status, status_desired, performed_at, status_reason, updated_at
		 FROM deployment_status WHERE artifact_uuid = ? AND organization_uuid = ? AND gateway_uuid = ?`),
		artifactUUID, org, gatewayUUID).
		Scan(&deploymentUUID, &status, &statusDesired, &performedAt, &statusReason, &updatedAt)
	if err != nil {
		return migrationcore.DeploymentStatusV1Row{}, err
	}
	return migrationcore.DeploymentStatusV1Row{
		ArtifactUUID: artifactUUID, Org: org, GatewayUUID: gatewayUUID, DeploymentUUID: deploymentUUID, Status: status,
		StatusDesired: statusDesired, StatusReason: statusReason, PerformedAt: performedAt, UpdatedAt: updatedAt,
	}, nil
}

// ---- api_keys (handle generated from name) ----

func readAPIKeyRow(v1 *database.DB, uuid string) (string, migrationcore.APIKeyV1Row, error) {
	var artifactUUID, name, maskedKey, apiKeyHashes, status, allowedTargets string
	var createdBy, issuer sql.NullString
	var createdAt, updatedAt, expiresAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT artifact_uuid, name, masked_api_key, api_key_hashes, status, created_at, created_by,
		        updated_at, expires_at, issuer, allowed_targets
		 FROM api_keys WHERE uuid = ?`), uuid).
		Scan(&artifactUUID, &name, &maskedKey, &apiKeyHashes, &status, &createdAt, &createdBy, &updatedAt, &expiresAt, &issuer, &allowedTargets)
	if err != nil {
		return "", migrationcore.APIKeyV1Row{}, err
	}
	return migrationcore.Slug(name), migrationcore.APIKeyV1Row{
		UUID: uuid, ArtifactUUID: artifactUUID, Name: name, MaskedKey: maskedKey,
		APIKeyHashes: apiKeyHashes, Status: status, AllowedTargets: allowedTargets, Issuer: issuer,
		CreatedAt: createdAt, UpdatedAt: updatedAt, ExpiresAt: expiresAt, CreatedBy: createdBy,
	}, nil
}

// ---- api_keys: resolve the uuid from the (artifact_uuid, name) natural key ----
// Update/Revoke/Delete take (artifactUUID, name); the mirror needs the uuid to read back
// or delete. Returns "" (no error) when the key is absent.

func resolveAPIKeyUUID(v1 *database.DB, artifactUUID, name string) (string, error) {
	var uuid string
	err := v1.QueryRow(v1.Rebind(
		`SELECT uuid FROM api_keys WHERE artifact_uuid = ? AND name = ?`), artifactUUID, name).Scan(&uuid)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return uuid, err
}

// ---- artifact uuid resolution from (handle, org, kind) for the type repos ----
// mcp/websub/webbroker/llm Delete take a handle; the mirror needs the uuid for DeleteArtifact.

func resolveArtifactUUID(v1 *database.DB, handle, org, kind string) (string, error) {
	var uuid string
	err := v1.QueryRow(v1.Rebind(
		`SELECT uuid FROM artifacts WHERE handle = ? AND organization_uuid = ? AND kind = ?`), handle, org, kind).Scan(&uuid)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return uuid, err
}

// ---- llm_provider_templates uuid resolution from (handle, org) ----

func resolveTemplateUUID(v1 *database.DB, handle, org string) (string, error) {
	var uuid string
	err := v1.QueryRow(v1.Rebind(
		`SELECT uuid FROM llm_provider_templates WHERE handle = ? AND organization_uuid = ?`), handle, org).Scan(&uuid)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return uuid, err
}

// ---- gateway_custom_policies uuid resolution from (org, name, version) ----

func resolveCustomPolicyUUID(v1 *database.DB, org, name, version string) (string, error) {
	var uuid string
	err := v1.QueryRow(v1.Rebind(
		`SELECT uuid FROM gateway_custom_policies WHERE organization_uuid = ? AND name = ? AND version = ?`),
		org, name, version).Scan(&uuid)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return uuid, err
}

// ---- mapping created_at read-backs (so the mirrored created_at matches the batch) ----

func readAppAPIKeyMappingRow(v1 *database.DB, appUUID, apiKeyID string) (migrationcore.ApplicationAPIKeyMappingV1Row, error) {
	var createdAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT created_at FROM application_api_keys WHERE application_uuid = ? AND api_key_id = ?`), appUUID, apiKeyID).Scan(&createdAt)
	if err != nil {
		return migrationcore.ApplicationAPIKeyMappingV1Row{}, err
	}
	return migrationcore.ApplicationAPIKeyMappingV1Row{ApplicationUUID: appUUID, APIKeyID: apiKeyID, CreatedAt: createdAt}, nil
}

func readAppArtifactMappingRow(v1 *database.DB, appUUID, artifactUUID string) (migrationcore.ApplicationArtifactMappingV1Row, error) {
	var createdAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT created_at FROM application_artifacts WHERE application_uuid = ? AND artifact_uuid = ?`), appUUID, artifactUUID).Scan(&createdAt)
	if err != nil {
		return migrationcore.ApplicationArtifactMappingV1Row{}, err
	}
	return migrationcore.ApplicationArtifactMappingV1Row{ApplicationUUID: appUUID, ArtifactUUID: artifactUUID, CreatedAt: createdAt}, nil
}

// ---- artifact_gateway_mappings (from association_mappings gateway rows) ----

func readArtifactGatewayMappingRow(v1 *database.DB, artifactUUID, org, gatewayUUID string) (migrationcore.ArtifactGatewayMappingV1Row, error) {
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT created_at, updated_at FROM association_mappings
		 WHERE artifact_uuid = ? AND resource_uuid = ? AND association_type = 'gateway' AND organization_uuid = ?`),
		artifactUUID, gatewayUUID, org).Scan(&createdAt, &updatedAt)
	if err != nil {
		return migrationcore.ArtifactGatewayMappingV1Row{}, err
	}
	return migrationcore.ArtifactGatewayMappingV1Row{
		ArtifactUUID: artifactUUID, Org: org, GatewayUUID: gatewayUUID, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}, nil
}

// ---- deployments: the id set for (artifact, gateway, org) ----
// CreateWithLimitEnforcement evicts the oldest archived deployments internally and returns
// only error, so the decorator diffs this set before/after the call to learn the evicted ids.

func readDeploymentIDs(v1 *database.DB, artifactUUID, gatewayUUID, org string) (map[string]bool, error) {
	rows, err := v1.Query(v1.Rebind(
		`SELECT deployment_id FROM deployments WHERE artifact_uuid = ? AND gateway_uuid = ? AND organization_uuid = ?`),
		artifactUUID, gatewayUUID, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	set := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		set[id] = true
	}
	return set, rows.Err()
}
