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
	"errors"
	"fmt"
	"strings"

	"github.com/wso2/api-platform/platform-api/internal/constants"
	"github.com/wso2/api-platform/platform-api/internal/model"
)

// sarg returns a nullable-string insert arg (value or nil for NULL).
func sarg(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

// insertArtifact writes the parent artifacts(uuid, type, organization_uuid) row.
func insertArtifact(ex Execer, opts Options, uuid, kind, org string) error {
	return upsert(ex, opts, "artifacts", []string{"uuid", "type", "organization_uuid"},
		[]any{uuid, kind, org}, []string{"uuid"})
}

// ---- organizations ----

// OrganizationV1Row is a faithful mirror of the v1 `organizations` table row
// (scan targets). It carries the raw v1 columns only — the v1→v2 mapping
// (name→display_name, null conversion, the idp placeholder, audit) lives in the
// transform below, so there is ONE place that mapping can be got wrong.
//
// The v1 organizations table has no created_by column, so this row carries none;
// the transform resolves created_by to the migration actor (audit with "").
//
// handle is the one genuinely caller-specific value (batch: carriedHandle —
// collision-safe + checkpointed; live: Slug), so it is passed to the transform
// separately rather than folded in.
type OrganizationV1Row struct {
	UUID, Name, Region   string
	CreatedAt, UpdatedAt sql.NullTime
}

// UpsertOrganizationV1 converts one v1 organizations row into its v2 footprint.
// handle is the resolved v2 handle supplied by the caller.
func UpsertOrganizationV1(ex Execer, handle string, r OrganizationV1Row, opts Options, rep Reporter) error {
	// idp_organization_ref_uuid = org uuid (deterministic placeholder).
	rep.Flag("organizations", r.UUID, FlagPlaceholderIDP, nil,
		map[string]any{"idp_organization_ref_uuid": r.UUID})
	createdBy, err := audit(ex, opts, rep, "organizations", r.UUID, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "organizations",
		[]string{"uuid", "handle", "display_name", "region", "idp_organization_ref_uuid",
			"data_version", "created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, handle, r.Name, r.Region, r.UUID, DataVersion, createdBy,
			tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- projects ----

// ProjectV1Row mirrors the v1 `projects` row. v1 projects has no handle column
// (the handle is generated from name by the caller) and no created_by column
// (the transform resolves it to the migration actor).
type ProjectV1Row struct {
	UUID, Name, Org      string
	Description          sql.NullString
	CreatedAt, UpdatedAt sql.NullTime
}

func UpsertProjectV1(ex Execer, handle string, r ProjectV1Row, opts Options, rep Reporter) error {
	createdBy, err := audit(ex, opts, rep, "projects", r.UUID, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "projects",
		[]string{"uuid", "handle", "display_name", "organization_uuid", "description",
			"data_version", "created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, handle, r.Name, r.Org, sarg(NullStrPtr(r.Description)), DataVersion, createdBy,
			tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- applications ----

type ApplicationV1Row struct {
	UUID, ProjectUUID, Org, Name, Type string
	Description                        sql.NullString
	CreatedAt, UpdatedAt               sql.NullTime
	CreatedBy                          sql.NullString
}

func UpsertApplicationV1(ex Execer, handle string, r ApplicationV1Row, opts Options, rep Reporter) error {
	createdBy, err := audit(ex, opts, rep, "applications", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	return upsert(ex, opts, "applications",
		[]string{"uuid", "handle", "project_uuid", "organization_uuid", "display_name", "description",
			"type", "data_version", "created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, handle, r.ProjectUUID, r.Org, r.Name, sarg(NullStrPtr(r.Description)), r.Type,
			DataVersion, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- rest_apis (RestApi) ----

type RestAPIV1Row struct {
	UUID, Name, Version, Org, ProjectUUID string
	Description, Lifecycle, Transport     sql.NullString
	Configuration                         []byte // v1 JSONB
	CreatedAt, UpdatedAt                  sql.NullTime
	CreatedBy                             sql.NullString
}

func UpsertRestAPIV1(ex Execer, handle string, r RestAPIV1Row, opts Options, rep Reporter) error {
	blob, _, unknown, err := ReshapeRestAPIConfig(r.Configuration, NullStr(r.Transport))
	if err != nil {
		rep.Quarantine("rest_apis", r.UUID, ReasonBlobUnparseable, err.Error(),
			map[string]any{"uuid": r.UUID})
		return ErrBlobUnparseable
	}
	rep.DroppedFields("RestAPIConfig", unknown)
	lc := lifecycleOr(NullStrPtr(r.Lifecycle))
	createdBy, err := audit(ex, opts, rep, "rest_apis", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	if err := insertArtifact(ex, opts, r.UUID, constants.RestApi, r.Org); err != nil {
		return err
	}
	return upsert(ex, opts, "rest_apis",
		[]string{"uuid", "organization_uuid", "handle", "display_name", "version", "project_uuid",
			"description", "lifecycle_status", "configuration", "data_version", "origin",
			"created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, r.Org, handle, r.Name, r.Version, r.ProjectUUID, sarg(NullStrPtr(r.Description)), lc, blob,
			DataVersion, OriginCP, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- llm_provider_templates ----

type LLMTemplateV1Row struct {
	UUID, Org, Name      string
	Description          sql.NullString
	Configuration        []byte // v1 TEXT
	CreatedAt, UpdatedAt sql.NullTime
	CreatedBy            sql.NullString
}

func UpsertLLMProviderTemplateV1(ex Execer, handle string, r LLMTemplateV1Row, opts Options, rep Reporter) error {
	groupID := handle // group_id = the template's v1 handle (matches v2 create path)
	rep.Flag("llm_provider_templates", r.UUID, FlagSynthesized, nil, map[string]any{
		"group_id": groupID, "version": "v1.0", "managed_by": constants.TemplateManagedByOrganization,
		"is_latest": 1, "enabled": 1, "openapi_spec": nil})
	createdBy, err := audit(ex, opts, rep, "llm_provider_templates", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	return upsert(ex, opts, "llm_provider_templates",
		[]string{"uuid", "organization_uuid", "handle", "group_id", "display_name", "managed_by",
			"version", "description", "configuration", "openapi_spec", "is_latest", "enabled",
			"data_version", "origin", "created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, r.Org, handle, groupID, r.Name, constants.TemplateManagedByOrganization,
			"v1.0", sarg(NullStrPtr(r.Description)), r.Configuration, nil, 1, 1,
			DataVersion, OriginCP, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- llm_providers (LlmProvider) ----

type LLMProviderV1Row struct {
	UUID, Name, Version, Org, TemplateUUID     string
	Description, OpenAPISpec, ModelList, Status sql.NullString
	Configuration                              []byte
	CreatedAt, UpdatedAt                       sql.NullTime
	CreatedBy                                  sql.NullString
}

func UpsertLLMProviderV1(ex Execer, handle string, r LLMProviderV1Row, opts Options, rep Reporter) error {
	var cfg model.LLMProviderConfig
	blob, unknown, err := RemarshalConfig(r.Configuration, &cfg)
	if err != nil {
		rep.Quarantine("llm_providers", r.UUID, ReasonBlobUnparseable, err.Error(), map[string]any{"uuid": r.UUID})
		return ErrBlobUnparseable
	}
	rep.DroppedFields("LLMProviderConfig", unknown)
	flagPlaintextCredential(rep, "llm_providers", r.UUID, cfg.Security)
	if r.Status.Valid && r.Status.String != "" {
		rep.Dropped("field", "llm_providers", r.UUID, DropLLMProviderStatus, r.Status.String)
	}
	createdBy, err := audit(ex, opts, rep, "llm_providers", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	if err := insertArtifact(ex, opts, r.UUID, constants.LLMProvider, r.Org); err != nil {
		return err
	}
	return upsert(ex, opts, "llm_providers",
		[]string{"uuid", "handle", "display_name", "version", "description", "template_uuid",
			"openapi_spec", "model_list", "configuration", "data_version", "origin",
			"created_by", "created_at", "updated_by", "updated_at", "organization_uuid"},
		[]any{r.UUID, handle, r.Name, r.Version, sarg(NullStrPtr(r.Description)), r.TemplateUUID,
			bytesOrNilP(NullStrPtr(r.OpenAPISpec)), bytesOrNilP(NullStrPtr(r.ModelList)), blob, DataVersion, OriginCP,
			createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts), r.Org},
		[]string{"uuid"})
}

// ---- llm_proxies (LlmProxy) ----

type LLMProxyV1Row struct {
	UUID, Name, Version, ProjectUUID, Org, ProviderUUID string
	Description, OpenAPISpec, Status                    sql.NullString
	Configuration                                       []byte
	CreatedAt, UpdatedAt                                sql.NullTime
	CreatedBy                                           sql.NullString
}

func UpsertLLMProxyV1(ex Execer, handle string, r LLMProxyV1Row, opts Options, rep Reporter) error {
	var cfg model.LLMProxyConfig
	blob, unknown, err := RemarshalConfig(r.Configuration, &cfg)
	if err != nil {
		rep.Quarantine("llm_proxies", r.UUID, ReasonBlobUnparseable, err.Error(), map[string]any{"uuid": r.UUID})
		return ErrBlobUnparseable
	}
	rep.DroppedFields("LLMProxyConfig", unknown)
	flagPlaintextCredential(rep, "llm_proxies", r.UUID, cfg.Security)
	if r.Status.Valid && r.Status.String != "" {
		rep.Dropped("field", "llm_proxies", r.UUID, DropLLMProxyStatus, r.Status.String)
	}
	createdBy, err := audit(ex, opts, rep, "llm_proxies", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	if err := insertArtifact(ex, opts, r.UUID, constants.LLMProxy, r.Org); err != nil {
		return err
	}
	return upsert(ex, opts, "llm_proxies",
		[]string{"uuid", "handle", "display_name", "version", "project_uuid", "description", "provider_uuid",
			"openapi_spec", "configuration", "data_version", "origin",
			"created_by", "created_at", "updated_by", "updated_at", "organization_uuid"},
		[]any{r.UUID, handle, r.Name, r.Version, r.ProjectUUID, sarg(NullStrPtr(r.Description)), r.ProviderUUID,
			bytesOrNilP(NullStrPtr(r.OpenAPISpec)), blob, DataVersion, OriginCP,
			createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts), r.Org},
		[]string{"uuid"})
}

// ---- mcp_proxies (Mcp) ----

type MCPProxyV1Row struct {
	UUID, Name, Version, Org         string
	ProjectUUID, Description, Status sql.NullString
	Configuration                    []byte
	CreatedAt, UpdatedAt             sql.NullTime
	CreatedBy                        sql.NullString
}

func UpsertMCPProxyV1(ex Execer, handle string, r MCPProxyV1Row, opts Options, rep Reporter) error {
	var cfg model.MCPProxyConfiguration
	blob, unknown, err := RemarshalConfig(r.Configuration, &cfg)
	if err != nil {
		rep.Quarantine("mcp_proxies", r.UUID, ReasonBlobUnparseable, err.Error(), map[string]any{"uuid": r.UUID})
		return ErrBlobUnparseable
	}
	rep.DroppedFields("MCPProxyConfiguration", unknown)
	if r.Status.Valid && r.Status.String != "" {
		rep.Dropped("field", "mcp_proxies", r.UUID, DropMCPStatus, r.Status.String)
	}
	createdBy, err := audit(ex, opts, rep, "mcp_proxies", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	if err := insertArtifact(ex, opts, r.UUID, constants.MCPProxy, r.Org); err != nil {
		return err
	}
	return upsert(ex, opts, "mcp_proxies",
		[]string{"uuid", "handle", "display_name", "version", "project_uuid", "description",
			"configuration", "data_version", "origin",
			"created_by", "created_at", "updated_by", "updated_at", "organization_uuid"},
		[]any{r.UUID, handle, r.Name, r.Version, sarg(NullStrPtr(r.ProjectUUID)), sarg(NullStrPtr(r.Description)),
			blob, DataVersion, OriginCP,
			createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts), r.Org},
		[]string{"uuid"})
}

// ---- websub_apis (WebSubApi, plugin table) ----

type WebSubV1Row struct {
	UUID, Name, Version, Org, ProjectUUID string
	Description, Lifecycle, Transport     sql.NullString
	Configuration                         []byte
	CreatedAt, UpdatedAt                  sql.NullTime
	CreatedBy                             sql.NullString
}

func UpsertWebSubAPIV1(ex Execer, handle string, r WebSubV1Row, opts Options, rep Reporter) error {
	blob, _, unknown, notes, err := ReshapeWebSubConfig(r.Configuration, NullStr(r.Transport))
	if err != nil {
		rep.Quarantine("websub_apis", r.UUID, ReasonBlobUnparseable, err.Error(), map[string]any{"uuid": r.UUID})
		return ErrBlobUnparseable
	}
	rep.DroppedFields("WebSubAPIConfiguration", unknown)
	if len(notes) > 0 {
		rep.Flag("websub_apis", r.UUID, FlagSynthesized, nil, map[string]any{"structural_reshape": notes})
	}
	lc := lifecycleOr(NullStrPtr(r.Lifecycle))
	createdBy, err := audit(ex, opts, rep, "websub_apis", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	if err := insertArtifact(ex, opts, r.UUID, constants.WebSubApi, r.Org); err != nil {
		return err
	}
	return upsert(ex, opts, "websub_apis",
		[]string{"uuid", "organization_uuid", "handle", "display_name", "version", "project_uuid",
			"description", "lifecycle_status", "configuration", "data_version", "origin",
			"created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, r.Org, handle, r.Name, r.Version, r.ProjectUUID, sarg(NullStrPtr(r.Description)), lc, blob,
			DataVersion, OriginCP, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- webbroker_apis (WebBrokerApi, plugin table) ----

type WebBrokerV1Row struct {
	UUID, Name, Version, Org, ProjectUUID string
	Description, Lifecycle, Transport     sql.NullString
	Configuration                         []byte
	CreatedAt, UpdatedAt                  sql.NullTime
	CreatedBy                             sql.NullString
}

func UpsertWebBrokerAPIV1(ex Execer, handle string, r WebBrokerV1Row, opts Options, rep Reporter) error {
	blob, _, unknown, err := ReshapeWebBrokerConfig(r.Configuration, NullStr(r.Transport))
	if err != nil {
		rep.Quarantine("webbroker_apis", r.UUID, ReasonBlobUnparseable, err.Error(), map[string]any{"uuid": r.UUID})
		return ErrBlobUnparseable
	}
	rep.DroppedFields("WebBrokerAPIConfiguration", unknown)
	lc := lifecycleOr(NullStrPtr(r.Lifecycle))
	createdBy, err := audit(ex, opts, rep, "webbroker_apis", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	if err := insertArtifact(ex, opts, r.UUID, constants.WebBrokerApi, r.Org); err != nil {
		return err
	}
	return upsert(ex, opts, "webbroker_apis",
		[]string{"uuid", "organization_uuid", "handle", "display_name", "version", "project_uuid",
			"description", "lifecycle_status", "configuration", "data_version", "origin",
			"created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, r.Org, handle, r.Name, r.Version, r.ProjectUUID, sarg(NullStrPtr(r.Description)), lc, blob,
			DataVersion, OriginCP, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- subscription_plans (+ subscription_plan_limits) ----

// SubscriptionPlanV1Row mirrors the v1 `subscription_plans` row. v1 has no handle
// column (generated from plan_name by the caller) and no created_by column.
type SubscriptionPlanV1Row struct {
	UUID, PlanName, Org, Status      string
	BillingPlan, ThrottleUnit        sql.NullString
	StopOnQuota                      sql.NullBool
	ThrottleCount                    sql.NullInt64
	ExpiryTime, CreatedAt, UpdatedAt sql.NullTime
}

// ErrUnmappedThrottleUnit is returned when a v1 throttle unit has no v2 mapping.
type ErrUnmappedThrottleUnit struct{ Unit string }

func (e ErrUnmappedThrottleUnit) Error() string {
	return "unmapped throttle_limit_unit " + e.Unit
}

func UpsertSubscriptionPlanV1(ex Execer, handle string, r SubscriptionPlanV1Row, opts Options, rep Reporter) error {
	if r.BillingPlan.Valid && r.BillingPlan.String != "" {
		rep.Dropped("field", "subscription_plans", r.UUID, DropBillingPlan, r.BillingPlan.String)
	}
	createdBy, err := audit(ex, opts, rep, "subscription_plans", r.UUID, "")
	if err != nil {
		return err
	}
	if err := upsert(ex, opts, "subscription_plans",
		[]string{"uuid", "handle", "display_name", "expiry_time", "organization_uuid", "status",
			"data_version", "created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, handle, r.PlanName, tsArg(NullTimePtr(r.ExpiryTime), opts), r.Org, r.Status, DataVersion,
			createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"}); err != nil {
		return err
	}

	// Throttle → exactly one subscription_plan_limits row. On a live UPDATE (§8.3) the
	// throttle unit may have changed (which shifts the row's natural key and would leave
	// the old limit behind) or the throttle may have been cleared: upsert the desired
	// limit, then delete any superseded limit rows so the v2 child state matches the v1
	// plan. Batch is InsertOnly, so the replacement deletes are skipped → byte-identical.
	if r.ThrottleCount.Valid && r.ThrottleUnit.Valid && r.ThrottleUnit.String != "" {
		timeUnit, ok := CaseConvertThrottleUnit(r.ThrottleUnit.String)
		if !ok {
			return ErrUnmappedThrottleUnit{Unit: r.ThrottleUnit.String}
		}
		limitTS := opts.Epoch
		if r.CreatedAt.Valid {
			limitTS = ReinterpretTZ(r.CreatedAt.Time, opts.loc())
		}
		limitUUID := DeterministicUUID(r.UUID+"|"+constants.LimitTypeRequestCount+"|"+timeUnit, limitTS)
		stop := 1
		if r.StopOnQuota.Valid {
			stop = BoolToSmallint(r.StopOnQuota.Bool)
		}
		if err := upsert(ex, opts, "subscription_plan_limits",
			[]string{"uuid", "subscription_plan_uuid", "limit_type", "time_unit", "time_amount",
				"limit_count", "limit_count_unit", "stop_on_quota_reach"},
			[]any{limitUUID, r.UUID, constants.LimitTypeRequestCount, timeUnit, 1, r.ThrottleCount.Int64, nil, stop},
			[]string{"subscription_plan_uuid", "limit_type", "time_amount", "time_unit"}); err != nil {
			return err
		}
		if !opts.InsertOnly && !opts.DryRun {
			if _, err := ex.Exec("DELETE FROM subscription_plan_limits WHERE subscription_plan_uuid = $1 AND uuid <> $2", r.UUID, limitUUID); err != nil {
				return fmt.Errorf("replace subscription_plan_limits for %s: %w", r.UUID, err)
			}
		}
	} else if !opts.InsertOnly && !opts.DryRun {
		// Throttle cleared on a live UPDATE → drop any stale limit rows.
		if _, err := ex.Exec("DELETE FROM subscription_plan_limits WHERE subscription_plan_uuid = $1", r.UUID); err != nil {
			return fmt.Errorf("clear subscription_plan_limits for %s: %w", r.UUID, err)
		}
	}
	return nil
}

// CaseConvertThrottleUnit maps a v1 PascalCase throttle unit to v2's UPPERCASE.
func CaseConvertThrottleUnit(u string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "min", "minute":
		return constants.ThrottleLimitUnitMinute, true
	case "hour":
		return constants.ThrottleLimitUnitHour, true
	case "day":
		return constants.ThrottleLimitUnitDay, true
	case "month":
		return constants.ThrottleLimitUnitMonth, true
	default:
		return "", false
	}
}

// ---- subscriptions ----

// SubscriptionV1Row mirrors the v1 `subscriptions` row (raw encrypted token +
// hash). No handle, no created_by column.
type SubscriptionV1Row struct {
	UUID, ArtifactUUID, SubscriberID, Token, Hash, Org, Status string
	ApplicationID, PlanUUID                                    sql.NullString
	CreatedAt, UpdatedAt                                       sql.NullTime
}

func UpsertSubscriptionV1(ex Execer, r SubscriptionV1Row, opts Options, rep Reporter) error {
	createdBy, err := audit(ex, opts, rep, "subscriptions", r.UUID, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "subscriptions",
		[]string{"uuid", "artifact_uuid", "subscriber_id", "application_id", "subscription_token",
			"subscription_token_hash", "subscription_plan_uuid", "organization_uuid", "status",
			"data_version", "created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, r.ArtifactUUID, r.SubscriberID, sarg(NullStrPtr(r.ApplicationID)), r.Token, r.Hash, sarg(NullStrPtr(r.PlanUUID)), r.Org, r.Status,
			DataVersion, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- gateways (+ gateway_endpoints) ----

// GatewayV1Row mirrors the v1 `gateways` row. v1 has BOTH a `name` column (the
// handle source, resolved to the v2 handle by the caller and passed in) and a real
// `display_name` column (kept here). No created_by column.
type GatewayV1Row struct {
	UUID, Org, DisplayName, Version, FuncType, Vhost string
	Description                                      sql.NullString
	Properties, Manifest                            []byte
	IsCritical, IsActive                            sql.NullBool
	CreatedAt, UpdatedAt                            sql.NullTime
}

func UpsertGatewayV1(ex Execer, handle string, r GatewayV1Row, opts Options, rep Reporter) error {
	ver, cut := TruncateStr(r.Version, 30)
	if cut {
		rep.Flag("gateways", r.UUID, FlagTruncated, map[string]any{"version": r.Version}, map[string]any{"version": ver})
	}
	createdBy, err := audit(ex, opts, rep, "gateways", r.UUID, "")
	if err != nil {
		return err
	}
	if err := upsert(ex, opts, "gateways",
		[]string{"uuid", "organization_uuid", "handle", "display_name", "description", "version",
			"gateway_functionality_type", "properties", "manifest", "is_active", "is_critical",
			"data_version", "created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, r.Org, handle, r.DisplayName, sarg(NullStrPtr(r.Description)), ver, r.FuncType, r.Properties, r.Manifest,
			BoolToSmallint(r.IsActive.Bool), BoolToSmallint(r.IsCritical.Bool),
			DataVersion, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"}); err != nil {
		return err
	}
	// vhost → exactly one gateway_endpoints row. On a live UPDATE (§8.3) the vhost may
	// have changed (gateway_endpoints has a SERIAL id and no natural key, so the changed
	// vhost inserts a second row) or been cleared: insert the desired endpoint, then
	// delete any superseded ones so the v2 child state matches the v1 gateway. Batch is
	// InsertOnly, so the replacement deletes are skipped → batch output is byte-identical.
	if strings.TrimSpace(r.Vhost) != "" {
		if err := UpsertGatewayEndpoint(ex, r.UUID, r.Vhost, opts); err != nil {
			return err
		}
		if !opts.InsertOnly && !opts.DryRun {
			if _, err := ex.Exec("DELETE FROM gateway_endpoints WHERE gateway_uuid = $1 AND url <> $2", r.UUID, r.Vhost); err != nil {
				return fmt.Errorf("replace gateway_endpoints for %s: %w", r.UUID, err)
			}
		}
		return nil
	}
	// vhost cleared on a live UPDATE → drop any stale endpoint rows.
	if !opts.InsertOnly && !opts.DryRun {
		if _, err := ex.Exec("DELETE FROM gateway_endpoints WHERE gateway_uuid = $1", r.UUID); err != nil {
			return fmt.Errorf("clear gateway_endpoints for %s: %w", r.UUID, err)
		}
	}
	return nil
}

// UpsertGatewayEndpoint inserts (gateway_uuid, url) once; gateway_endpoints has a
// SERIAL id and no natural unique key, so idempotency is an explicit existence check.
// Returns whether a row was inserted.
func UpsertGatewayEndpoint(ex Execer, gatewayUUID, url string, opts Options) error {
	if opts.DryRun {
		return nil
	}
	var one int
	err := ex.QueryRow("SELECT 1 FROM gateway_endpoints WHERE gateway_uuid = $1 AND url = $2", gatewayUUID, url).Scan(&one)
	if err == nil {
		return nil // already present
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return upsert(ex, opts, "gateway_endpoints", []string{"gateway_uuid", "url"}, []any{gatewayUUID, url}, nil)
}

// ---- artifact_gateway_mappings ----

type ArtifactGatewayMappingV1Row struct {
	ArtifactUUID, Org, GatewayUUID string
	CreatedAt, UpdatedAt           sql.NullTime
}

func UpsertArtifactGatewayMappingV1(ex Execer, r ArtifactGatewayMappingV1Row, opts Options, rep Reporter) error {
	key := r.ArtifactUUID + "|" + r.GatewayUUID
	createdBy, err := audit(ex, opts, rep, "artifact_gateway_mappings", key, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "artifact_gateway_mappings",
		[]string{"artifact_uuid", "organization_uuid", "gateway_uuid", "metadata",
			"created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.ArtifactUUID, r.Org, r.GatewayUUID, nil, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"organization_uuid", "artifact_uuid", "gateway_uuid"})
}

// ---- gateway_custom_policies ----

// GatewayCustomPolicyV1Row mirrors the v1 `gateway_custom_policies` row. This table
// keys on (name, version), not a handle; no created_by column.
type GatewayCustomPolicyV1Row struct {
	UUID, Org, Name, Version string
	DisplayName, Description sql.NullString
	PolicyDefinition         []byte
	CreatedAt, UpdatedAt     sql.NullTime
}

func UpsertGatewayCustomPolicyV1(ex Execer, r GatewayCustomPolicyV1Row, opts Options, rep Reporter) error {
	var descVal any
	if r.Description.Valid {
		d, cut := TruncateStr(r.Description.String, 1023)
		if cut {
			rep.Flag("gateway_custom_policies", r.UUID, FlagTruncated,
				map[string]any{"description_len": len(r.Description.String)}, map[string]any{"description_len": len(d)})
		}
		descVal = d
	}
	createdBy, err := audit(ex, opts, rep, "gateway_custom_policies", r.UUID, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "gateway_custom_policies",
		[]string{"uuid", "organization_uuid", "name", "display_name", "version", "description",
			"policy_definition", "data_version", "created_by", "created_at", "updated_by", "updated_at"},
		[]any{r.UUID, r.Org, r.Name, sarg(NullStrPtr(r.DisplayName)), r.Version, descVal, r.PolicyDefinition, DataVersion,
			createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"uuid"})
}

// ---- gateway_custom_policy_usages ----

type PolicyUsageV1Row struct{ PolicyUUID, ArtifactUUID string }

func UpsertPolicyUsageV1(ex Execer, r PolicyUsageV1Row, opts Options, rep Reporter) error {
	return upsert(ex, opts, "gateway_custom_policy_usages",
		[]string{"policy_uuid", "artifact_uuid"}, []any{r.PolicyUUID, r.ArtifactUUID},
		[]string{"policy_uuid", "artifact_uuid"})
}

// ---- gateway_tokens ----

type GatewayTokenV1Row struct {
	UUID, GatewayUUID, TokenHash, Salt, Status string
	CreatedAt, RevokedAt                       sql.NullTime
}

func UpsertGatewayTokenV1(ex Execer, r GatewayTokenV1Row, opts Options, rep Reporter) error {
	createdBy, err := audit(ex, opts, rep, "gateway_tokens", r.UUID, "")
	if err != nil {
		return err
	}
	var revokedBy any
	if r.RevokedAt.Valid {
		actor, _, e := ResolveIdentity(ex, MigrationActorIDPID, opts)
		if e != nil {
			return e
		}
		revokedBy = actor
	}
	return upsert(ex, opts, "gateway_tokens",
		[]string{"uuid", "gateway_uuid", "token_hash", "salt", "status", "data_version",
			"created_by", "created_at", "revoked_by", "revoked_at"},
		[]any{r.UUID, r.GatewayUUID, r.TokenHash, r.Salt, r.Status, DataVersion,
			createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), revokedBy, tsArg(NullTimePtr(r.RevokedAt), opts)},
		[]string{"uuid"})
}

// ---- deployments ----

// DeploymentV1Row mirrors the v1 `deployments` row. No created_by column.
// BaseDeploymentUUID is the caller-resolved base link (the batch drops it when the
// predecessor was not migrated), not a raw column, so it stays *string.
type DeploymentV1Row struct {
	UUID, Name, ArtifactUUID, Org, GatewayUUID string
	BaseDeploymentUUID                         *string
	Metadata                                   sql.NullString
	Content                                    []byte
	CreatedAt                                  sql.NullTime
}

func UpsertDeploymentV1(ex Execer, r DeploymentV1Row, opts Options, rep Reporter) error {
	createdBy, err := audit(ex, opts, rep, "deployments", r.UUID, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "deployments",
		[]string{"uuid", "display_name", "artifact_uuid", "organization_uuid", "gateway_uuid",
			"base_deployment_uuid", "content", "metadata", "data_version", "created_by", "created_at"},
		[]any{r.UUID, r.Name, r.ArtifactUUID, r.Org, r.GatewayUUID, sarg(r.BaseDeploymentUUID),
			r.Content, bytesOrNilP(NullStrPtr(r.Metadata)), DataVersion, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts)},
		[]string{"uuid"})
}

// ---- deployment_status ----

// DeploymentStatusV1Row mirrors the v1 `deployment_status` row. No performed_by
// column (resolved to the migration actor).
type DeploymentStatusV1Row struct {
	ArtifactUUID, Org, GatewayUUID, DeploymentUUID, Status string
	StatusDesired, StatusReason                            sql.NullString
	PerformedAt, UpdatedAt                                 sql.NullTime
}

func UpsertDeploymentStatusV1(ex Execer, r DeploymentStatusV1Row, opts Options, rep Reporter) error {
	key := r.ArtifactUUID + "|" + r.Org + "|" + r.GatewayUUID
	performedBy, err := audit(ex, opts, rep, "deployment_status", key, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "deployment_status",
		[]string{"artifact_uuid", "organization_uuid", "gateway_uuid", "deployment_uuid", "status",
			"status_desired", "performed_at", "performed_by", "status_reason", "updated_at"},
		[]any{r.ArtifactUUID, r.Org, r.GatewayUUID, r.DeploymentUUID, r.Status, sarg(NullStrPtr(r.StatusDesired)),
			tsArg(NullTimePtr(r.PerformedAt), opts), performedBy, sarg(NullStrPtr(r.StatusReason)), tsArg(NullTimePtr(r.UpdatedAt), opts)},
		[]string{"organization_uuid", "artifact_uuid", "gateway_uuid"})
}

// ---- api_keys ----

// APIKeyV1Row mirrors the v1 `api_keys` row. The handle is generated from name by
// the caller and passed in; Name feeds v2 display_name.
type APIKeyV1Row struct {
	UUID, ArtifactUUID, Name, MaskedKey, APIKeyHashes, Status, AllowedTargets string
	Issuer                                                                    sql.NullString
	CreatedAt, UpdatedAt, ExpiresAt                                           sql.NullTime
	CreatedBy                                                                 sql.NullString
}

func UpsertAPIKeyV1(ex Execer, handle string, r APIKeyV1Row, opts Options, rep Reporter) error {
	var issuerVal any
	if r.Issuer.Valid {
		iv, cut := TruncateStr(r.Issuer.String, 255)
		if cut {
			rep.Flag("api_keys", r.UUID, FlagTruncated, map[string]any{"issuer_len": len(r.Issuer.String)}, map[string]any{"issuer_len": len(iv)})
		}
		issuerVal = iv
	}
	at, cut := TruncateStr(r.AllowedTargets, 255)
	if cut {
		rep.Flag("api_keys", r.UUID, FlagTruncated, map[string]any{"allowed_targets_len": len(r.AllowedTargets)}, map[string]any{"allowed_targets_len": len(at)})
	}
	createdBy, err := audit(ex, opts, rep, "api_keys", r.UUID, NullStr(r.CreatedBy))
	if err != nil {
		return err
	}
	return upsert(ex, opts, "api_keys",
		[]string{"uuid", "artifact_uuid", "handle", "display_name", "masked_api_key", "api_key_hashes",
			"status", "data_version", "created_by", "created_at", "updated_by", "updated_at",
			"expires_at", "issuer", "allowed_targets"},
		[]any{r.UUID, r.ArtifactUUID, handle, r.Name, r.MaskedKey, []byte(r.APIKeyHashes), r.Status, DataVersion,
			createdBy, tsArg(NullTimePtr(r.CreatedAt), opts), createdBy, tsArg(NullTimePtr(r.UpdatedAt), opts), tsArg(NullTimePtr(r.ExpiresAt), opts), issuerVal, at},
		[]string{"uuid"})
}

// ---- application_api_key_mappings ----

type ApplicationAPIKeyMappingV1Row struct {
	ApplicationUUID, APIKeyID string
	CreatedAt                 sql.NullTime
}

func UpsertApplicationAPIKeyMappingV1(ex Execer, r ApplicationAPIKeyMappingV1Row, opts Options, rep Reporter) error {
	key := r.ApplicationUUID + "|" + r.APIKeyID
	createdBy, err := audit(ex, opts, rep, "application_api_key_mappings", key, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "application_api_key_mappings",
		[]string{"application_uuid", "api_key_id", "created_by", "created_at"},
		[]any{r.ApplicationUUID, r.APIKeyID, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts)},
		[]string{"application_uuid", "api_key_id"})
}

// ---- application_artifact_mappings ----

type ApplicationArtifactMappingV1Row struct {
	ApplicationUUID, ArtifactUUID string
	CreatedAt                     sql.NullTime
}

func UpsertApplicationArtifactMappingV1(ex Execer, r ApplicationArtifactMappingV1Row, opts Options, rep Reporter) error {
	key := r.ApplicationUUID + "|" + r.ArtifactUUID
	createdBy, err := audit(ex, opts, rep, "application_artifact_mappings", key, "")
	if err != nil {
		return err
	}
	return upsert(ex, opts, "application_artifact_mappings",
		[]string{"application_uuid", "artifact_uuid", "created_by", "created_at"},
		[]any{r.ApplicationUUID, r.ArtifactUUID, createdBy, tsArg(NullTimePtr(r.CreatedAt), opts)},
		[]string{"application_uuid", "artifact_uuid"})
}

// ---- shared helpers ----

func lifecycleOr(p *string) string {
	if p != nil && *p != "" {
		return *p
	}
	return "CREATED"
}

func bytesOrNilP(p *string) any {
	if p == nil {
		return nil
	}
	return []byte(*p)
}

func flagPlaintextCredential(rep Reporter, table, key string, sec *model.SecurityConfig) {
	if sec != nil && sec.APIKey != nil && sec.APIKey.Key != "" {
		rep.Flag(table, key, FlagPlaintextCredential, nil,
			map[string]any{"note": "upstream apiKey.key stored in plaintext in the config blob"})
	}
}
