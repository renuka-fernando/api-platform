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
	"time"

	"platform-api/src/internal/database"

	"github.com/wso2/api-platform/platform-api/migrationcore"
)

// nsp / ntp / strv mirror cmd/dbmigrate: convert scanned nullable columns to the pointer /
// value forms the migrationcore rows use.
func nsp(ns sql.NullString) *string {
	if ns.Valid {
		s := ns.String
		return &s
	}
	return nil
}

func ntp(nt sql.NullTime) *time.Time {
	if nt.Valid {
		t := nt.Time
		return &t
	}
	return nil
}

func strv(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// ---- organizations ----

func readOrganizationRow(v1 *database.DB, uuid string) (migrationcore.OrganizationRow, error) {
	var handle, name, region string
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT handle, name, region, created_at, updated_at FROM organizations WHERE uuid = ?`), uuid).
		Scan(&handle, &name, &region, &createdAt, &updatedAt)
	if err != nil {
		return migrationcore.OrganizationRow{}, err
	}
	return migrationcore.OrganizationRow{
		UUID: uuid, Handle: migrationcore.Slug(handle), DisplayName: name, Region: region,
		CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt),
	}, nil
}

// ---- projects (no v1 handle/created_by; handle generated from name) ----

func readProjectRow(v1 *database.DB, uuid string) (migrationcore.ProjectRow, error) {
	var name, org string
	var description sql.NullString
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT name, organization_uuid, description, created_at, updated_at FROM projects WHERE uuid = ?`), uuid).
		Scan(&name, &org, &description, &createdAt, &updatedAt)
	if err != nil {
		return migrationcore.ProjectRow{}, err
	}
	return migrationcore.ProjectRow{
		UUID: uuid, Handle: migrationcore.Slug(name), DisplayName: name, Org: org, Description: nsp(description),
		CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt),
	}, nil
}

// ---- applications ----

func readApplicationRow(v1 *database.DB, uuid string) (migrationcore.ApplicationRow, error) {
	var handle, projectUUID, org, name, typ string
	var createdBy, description sql.NullString
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT handle, project_uuid, organization_uuid, created_by, name, description, type, created_at, updated_at
		 FROM applications WHERE uuid = ?`), uuid).
		Scan(&handle, &projectUUID, &org, &createdBy, &name, &description, &typ, &createdAt, &updatedAt)
	if err != nil {
		return migrationcore.ApplicationRow{}, err
	}
	return migrationcore.ApplicationRow{
		UUID: uuid, Handle: migrationcore.Slug(handle), ProjectUUID: projectUUID, Org: org, DisplayName: name, Type: typ,
		Description: nsp(description), CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), CreatedBy: strv(createdBy),
	}, nil
}

// ---- rest_apis ----

func readRestAPIRow(v1 *database.DB, uuid string) (migrationcore.RestAPIRow, error) {
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
		return migrationcore.RestAPIRow{}, err
	}
	return migrationcore.RestAPIRow{
		UUID: uuid, Handle: migrationcore.Slug(handle), DisplayName: name, Version: version, Org: org, ProjectUUID: projectUUID,
		Description: nsp(description), Lifecycle: nsp(lifecycle), Transport: nsp(transport), Configuration: config,
		CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), CreatedBy: strv(createdBy),
	}, nil
}

// ---- llm_provider_templates ----

func readLLMTemplateRow(v1 *database.DB, uuid string) (migrationcore.LLMTemplateRow, error) {
	var org, handle, name string
	var description, createdBy sql.NullString
	var config []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT organization_uuid, handle, name, description, created_by, configuration, created_at, updated_at
		 FROM llm_provider_templates WHERE uuid = ?`), uuid).
		Scan(&org, &handle, &name, &description, &createdBy, &config, &createdAt, &updatedAt)
	if err != nil {
		return migrationcore.LLMTemplateRow{}, err
	}
	return migrationcore.LLMTemplateRow{
		UUID: uuid, Org: org, Handle: migrationcore.Slug(handle), DisplayName: name, Description: nsp(description),
		Configuration: config, CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), CreatedBy: strv(createdBy),
	}, nil
}

// ---- llm_providers ----

func readLLMProviderRow(v1 *database.DB, uuid string) (migrationcore.LLMProviderRow, error) {
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
		return migrationcore.LLMProviderRow{}, err
	}
	return migrationcore.LLMProviderRow{
		UUID: uuid, Handle: migrationcore.Slug(handle), DisplayName: name, Version: version, Org: org, TemplateUUID: templateUUID,
		Description: nsp(description), OpenAPISpec: nsp(openapiSpec), ModelList: nsp(modelList), Status: nsp(status),
		Configuration: config, CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), CreatedBy: strv(createdBy),
	}, nil
}

// ---- llm_proxies ----

func readLLMProxyRow(v1 *database.DB, uuid string) (migrationcore.LLMProxyRow, error) {
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
		return migrationcore.LLMProxyRow{}, err
	}
	return migrationcore.LLMProxyRow{
		UUID: uuid, Handle: migrationcore.Slug(handle), DisplayName: name, Version: version, ProjectUUID: projectUUID, Org: org, ProviderUUID: providerUUID,
		Description: nsp(description), OpenAPISpec: nsp(openapiSpec), Status: nsp(status),
		Configuration: config, CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), CreatedBy: strv(createdBy),
	}, nil
}

// ---- mcp_proxies ----

func readMCPProxyRow(v1 *database.DB, uuid string) (migrationcore.MCPProxyRow, error) {
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
		return migrationcore.MCPProxyRow{}, err
	}
	return migrationcore.MCPProxyRow{
		UUID: uuid, Handle: migrationcore.Slug(handle), DisplayName: name, Version: version, Org: org,
		ProjectUUID: nsp(projectUUID), Description: nsp(description), Status: nsp(status),
		Configuration: config, CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), CreatedBy: strv(createdBy),
	}, nil
}

// ---- websub_apis ----

func readWebSubRow(v1 *database.DB, uuid string) (migrationcore.WebSubRow, error) {
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
		return migrationcore.WebSubRow{}, err
	}
	return migrationcore.WebSubRow{
		UUID: uuid, Handle: migrationcore.Slug(handle), DisplayName: name, Version: version, Org: org, ProjectUUID: projectUUID,
		Description: nsp(description), Lifecycle: nsp(lifecycle), Transport: nsp(transport), Configuration: config,
		CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), CreatedBy: strv(createdBy),
	}, nil
}

// ---- webbroker_apis ----

func readWebBrokerRow(v1 *database.DB, uuid string) (migrationcore.WebBrokerRow, error) {
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
		return migrationcore.WebBrokerRow{}, err
	}
	return migrationcore.WebBrokerRow{
		UUID: uuid, Handle: migrationcore.Slug(handle), DisplayName: name, Version: version, Org: org, ProjectUUID: projectUUID,
		Description: nsp(description), Lifecycle: nsp(lifecycle), Transport: nsp(transport), Configuration: config,
		CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), CreatedBy: strv(createdBy),
	}, nil
}

// ---- subscription_plans (no v1 handle/created_by; handle generated from plan_name) ----

func readSubscriptionPlanRow(v1 *database.DB, uuid string) (migrationcore.SubscriptionPlanRow, error) {
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
		return migrationcore.SubscriptionPlanRow{}, err
	}
	row := migrationcore.SubscriptionPlanRow{
		UUID: uuid, Handle: migrationcore.Slug(planName), DisplayName: planName, Org: org, Status: status,
		BillingPlan: nsp(billingPlan), ThrottleUnit: nsp(throttleUnit),
		ExpiryTime: ntp(expiry), CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt),
	}
	if stopOnQuota.Valid {
		b := stopOnQuota.Bool
		row.StopOnQuota = &b
	}
	if throttleCount.Valid {
		c := throttleCount.Int64
		row.ThrottleCount = &c
	}
	return row, nil
}

// ---- subscriptions (token/hash are read raw: the model carries only the DECRYPTED token) ----

func readSubscriptionRow(v1 *database.DB, uuid string) (migrationcore.SubscriptionRow, error) {
	var apiUUID, subscriberID, token, hash, org, status string
	var applicationID, planUUID sql.NullString
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT api_uuid, subscriber_id, application_id, subscription_token, subscription_token_hash,
		        subscription_plan_uuid, organization_uuid, status, created_at, updated_at
		 FROM subscriptions WHERE uuid = ?`), uuid).
		Scan(&apiUUID, &subscriberID, &applicationID, &token, &hash, &planUUID, &org, &status, &createdAt, &updatedAt)
	if err != nil {
		return migrationcore.SubscriptionRow{}, err
	}
	return migrationcore.SubscriptionRow{
		UUID: uuid, ArtifactUUID: apiUUID, SubscriberID: subscriberID, Token: token, Hash: hash, Org: org, Status: status,
		ApplicationID: nsp(applicationID), PlanUUID: nsp(planUUID), CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt),
	}, nil
}

// ---- gateways (properties + manifest are read raw; handle generated from name) ----

func readGatewayRow(v1 *database.DB, uuid string) (migrationcore.GatewayRow, error) {
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
		return migrationcore.GatewayRow{}, err
	}
	return migrationcore.GatewayRow{
		UUID: uuid, Org: org, Handle: migrationcore.Slug(name), DisplayName: displayName, Version: version,
		FuncType: funcType, Vhost: vhost, Description: nsp(description), Properties: properties, Manifest: manifest,
		IsCritical: isCritical.Bool, IsActive: isActive.Bool, CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt),
	}, nil
}

// ---- gateway_tokens ----

func readGatewayTokenRow(v1 *database.DB, uuid string) (migrationcore.GatewayTokenRow, error) {
	var gatewayUUID, tokenHash, salt, status string
	var createdAt, revokedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT gateway_uuid, token_hash, salt, status, created_at, revoked_at FROM gateway_tokens WHERE uuid = ?`), uuid).
		Scan(&gatewayUUID, &tokenHash, &salt, &status, &createdAt, &revokedAt)
	if err != nil {
		return migrationcore.GatewayTokenRow{}, err
	}
	return migrationcore.GatewayTokenRow{
		UUID: uuid, GatewayUUID: gatewayUUID, TokenHash: tokenHash, Salt: salt, Status: status,
		CreatedAt: ntp(createdAt), RevokedAt: ntp(revokedAt),
	}, nil
}

// ---- gateway_custom_policies ----

func readGatewayCustomPolicyRow(v1 *database.DB, uuid string) (migrationcore.GatewayCustomPolicyRow, error) {
	var org, name, version string
	var displayName, description sql.NullString
	var policyDef []byte
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT organization_uuid, name, display_name, version, description, policy_definition, created_at, updated_at
		 FROM gateway_custom_policies WHERE uuid = ?`), uuid).
		Scan(&org, &name, &displayName, &version, &description, &policyDef, &createdAt, &updatedAt)
	if err != nil {
		return migrationcore.GatewayCustomPolicyRow{}, err
	}
	return migrationcore.GatewayCustomPolicyRow{
		UUID: uuid, Org: org, Name: name, Version: version, DisplayName: nsp(displayName), Description: nsp(description),
		PolicyDefinition: policyDef, CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt),
	}, nil
}

// ---- deployments ----

func readDeploymentRow(v1 *database.DB, deploymentID string) (migrationcore.DeploymentRow, error) {
	var name, artifactUUID, org, gatewayUUID string
	var baseDeployment, metadata sql.NullString
	var content []byte
	var createdAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT name, artifact_uuid, organization_uuid, gateway_uuid, base_deployment_id, content, metadata, created_at
		 FROM deployments WHERE deployment_id = ?`), deploymentID).
		Scan(&name, &artifactUUID, &org, &gatewayUUID, &baseDeployment, &content, &metadata, &createdAt)
	if err != nil {
		return migrationcore.DeploymentRow{}, err
	}
	return migrationcore.DeploymentRow{
		UUID: deploymentID, DisplayName: name, ArtifactUUID: artifactUUID, Org: org, GatewayUUID: gatewayUUID,
		BaseDeploymentUUID: nsp(baseDeployment), Metadata: nsp(metadata), Content: content, CreatedAt: ntp(createdAt),
	}, nil
}

// ---- deployment_status (current state per artifact+org+gateway) ----

func readDeploymentStatusRow(v1 *database.DB, artifactUUID, org, gatewayUUID string) (migrationcore.DeploymentStatusRow, error) {
	var deploymentUUID, status string
	var statusDesired, statusReason sql.NullString
	var performedAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT deployment_id, status, status_desired, performed_at, status_reason, updated_at
		 FROM deployment_status WHERE artifact_uuid = ? AND organization_uuid = ? AND gateway_uuid = ?`),
		artifactUUID, org, gatewayUUID).
		Scan(&deploymentUUID, &status, &statusDesired, &performedAt, &statusReason, &updatedAt)
	if err != nil {
		return migrationcore.DeploymentStatusRow{}, err
	}
	return migrationcore.DeploymentStatusRow{
		ArtifactUUID: artifactUUID, Org: org, GatewayUUID: gatewayUUID, DeploymentUUID: deploymentUUID, Status: status,
		StatusDesired: nsp(statusDesired), StatusReason: nsp(statusReason), PerformedAt: ntp(performedAt), UpdatedAt: ntp(updatedAt),
	}, nil
}

// ---- api_keys (handle generated from name) ----

func readAPIKeyRow(v1 *database.DB, uuid string) (migrationcore.APIKeyRow, error) {
	var artifactUUID, name, maskedKey, apiKeyHashes, status, allowedTargets string
	var createdBy, issuer sql.NullString
	var createdAt, updatedAt, expiresAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT artifact_uuid, name, masked_api_key, api_key_hashes, status, created_at, created_by,
		        updated_at, expires_at, issuer, allowed_targets
		 FROM api_keys WHERE uuid = ?`), uuid).
		Scan(&artifactUUID, &name, &maskedKey, &apiKeyHashes, &status, &createdAt, &createdBy, &updatedAt, &expiresAt, &issuer, &allowedTargets)
	if err != nil {
		return migrationcore.APIKeyRow{}, err
	}
	return migrationcore.APIKeyRow{
		UUID: uuid, ArtifactUUID: artifactUUID, Handle: migrationcore.Slug(name), DisplayName: name, MaskedKey: maskedKey,
		APIKeyHashes: apiKeyHashes, Status: status, AllowedTargets: allowedTargets, Issuer: nsp(issuer),
		CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt), ExpiresAt: ntp(expiresAt), CreatedBy: strv(createdBy),
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

func readAppAPIKeyMappingRow(v1 *database.DB, appUUID, apiKeyID string) (migrationcore.ApplicationAPIKeyMappingRow, error) {
	var createdAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT created_at FROM application_api_keys WHERE application_uuid = ? AND api_key_id = ?`), appUUID, apiKeyID).Scan(&createdAt)
	if err != nil {
		return migrationcore.ApplicationAPIKeyMappingRow{}, err
	}
	return migrationcore.ApplicationAPIKeyMappingRow{ApplicationUUID: appUUID, APIKeyID: apiKeyID, CreatedAt: ntp(createdAt)}, nil
}

func readAppArtifactMappingRow(v1 *database.DB, appUUID, artifactUUID string) (migrationcore.ApplicationArtifactMappingRow, error) {
	var createdAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT created_at FROM application_artifacts WHERE application_uuid = ? AND artifact_uuid = ?`), appUUID, artifactUUID).Scan(&createdAt)
	if err != nil {
		return migrationcore.ApplicationArtifactMappingRow{}, err
	}
	return migrationcore.ApplicationArtifactMappingRow{ApplicationUUID: appUUID, ArtifactUUID: artifactUUID, CreatedAt: ntp(createdAt)}, nil
}

// ---- artifact_gateway_mappings (from association_mappings gateway rows) ----

func readArtifactGatewayMappingRow(v1 *database.DB, artifactUUID, org, gatewayUUID string) (migrationcore.ArtifactGatewayMappingRow, error) {
	var createdAt, updatedAt sql.NullTime
	err := v1.QueryRow(v1.Rebind(
		`SELECT created_at, updated_at FROM association_mappings
		 WHERE artifact_uuid = ? AND resource_uuid = ? AND association_type = 'gateway' AND organization_uuid = ?`),
		artifactUUID, gatewayUUID, org).Scan(&createdAt, &updatedAt)
	if err != nil {
		return migrationcore.ArtifactGatewayMappingRow{}, err
	}
	return migrationcore.ArtifactGatewayMappingRow{
		ArtifactUUID: artifactUUID, Org: org, GatewayUUID: gatewayUUID, CreatedAt: ntp(createdAt), UpdatedAt: ntp(updatedAt),
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
