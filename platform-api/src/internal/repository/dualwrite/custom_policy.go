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

import (
	"platform-api/src/internal/model"
	"platform-api/src/internal/repository"

	"github.com/wso2/api-platform/platform-api/migrationcore"
)

// ---- CustomPolicyRepository ----

type customPolicyRepo struct {
	repository.CustomPolicyRepository
	sink *Sink
}

// NewCustomPolicyRepo wraps a v1 CustomPolicyRepository with v2 mirroring.
func NewCustomPolicyRepo(inner repository.CustomPolicyRepository, sink *Sink) repository.CustomPolicyRepository {
	return &customPolicyRepo{CustomPolicyRepository: inner, sink: sink}
}

// upsertCustomPolicy mirrors the full policy read back by uuid.
func (d *customPolicyRepo) upsertCustomPolicy(uuid, org string) {
	d.sink.mirrorResolvedUpsert("gateway_custom_policy", "gateway_custom_policies", uuid, org, func(ex migrationcore.Execer) error {
		row, err := readGatewayCustomPolicyRow(d.sink.v1, uuid)
		if err != nil {
			return err
		}
		return migrationcore.UpsertGatewayCustomPolicyV1(ex, row, d.sink.opts, d.sink.reporter)
	})
}

func (d *customPolicyRepo) InsertCustomPolicy(policy *model.CustomPolicy) error {
	if err := d.CustomPolicyRepository.InsertCustomPolicy(policy); err != nil {
		return err
	}
	// InsertCustomPolicy is ON CONFLICT(org,name,version) — on conflict the row keeps its
	// ORIGINAL uuid, so resolve the stored uuid by the natural key rather than trusting
	// policy.UUID.
	uuid, _ := resolveCustomPolicyUUID(d.sink.v1, policy.OrganizationUUID, policy.Name, policy.Version)
	d.upsertCustomPolicy(uuid, policy.OrganizationUUID)
	return nil
}

func (d *customPolicyRepo) UpdateCustomPolicy(policy *model.CustomPolicy, oldVersion string) error {
	if err := d.CustomPolicyRepository.UpdateCustomPolicy(policy, oldVersion); err != nil {
		return err
	}
	// The row now lives at the NEW version; resolve by (org, name, new version).
	uuid, _ := resolveCustomPolicyUUID(d.sink.v1, policy.OrganizationUUID, policy.Name, policy.Version)
	d.upsertCustomPolicy(uuid, policy.OrganizationUUID)
	return nil
}

func (d *customPolicyRepo) DeleteCustomPolicy(orgUUID, name, version string) error {
	uuid, _ := resolveCustomPolicyUUID(d.sink.v1, orgUUID, name, version) // resolve BEFORE the delete
	if err := d.CustomPolicyRepository.DeleteCustomPolicy(orgUUID, name, version); err != nil {
		return err
	}
	d.sink.mirrorResolvedDelete("gateway_custom_policy", "gateway_custom_policies", uuid, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteGatewayCustomPolicy(ex, d.sink.opts, uuid)
	})
	return nil
}

func (d *customPolicyRepo) DeleteCustomPolicyIfUnused(orgUUID, policyUUID string) error {
	// v1 returns nil ONLY when it actually deleted the row (else ErrCustomPolicyNotFound /
	// ErrCustomPolicyInUse). Mirror the delete only on that confirmed deletion.
	if err := d.CustomPolicyRepository.DeleteCustomPolicyIfUnused(orgUUID, policyUUID); err != nil {
		return err
	}
	d.sink.mirrorDelete("gateway_custom_policy", "gateway_custom_policies", policyUUID, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteGatewayCustomPolicy(ex, d.sink.opts, policyUUID)
	})
	return nil
}

func (d *customPolicyRepo) InsertCustomPolicyUsage(policyUUID, apiUUID string) error {
	if err := d.CustomPolicyRepository.InsertCustomPolicyUsage(policyUUID, apiUUID); err != nil {
		return err
	}
	d.sink.mirrorUpsert("policy_usage", "gateway_custom_policy_usages", policyUUID+"|"+apiUUID, "", func(ex migrationcore.Execer) error {
		return migrationcore.UpsertPolicyUsageV1(ex, migrationcore.PolicyUsageV1Row{PolicyUUID: policyUUID, ArtifactUUID: apiUUID}, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *customPolicyRepo) DeleteCustomPolicyUsage(policyUUID, apiUUID string) error {
	if err := d.CustomPolicyRepository.DeleteCustomPolicyUsage(policyUUID, apiUUID); err != nil {
		return err
	}
	d.sink.mirrorDelete("policy_usage", "gateway_custom_policy_usages", policyUUID+"|"+apiUUID, "", func(ex migrationcore.Execer) error {
		return migrationcore.DeletePolicyUsage(ex, d.sink.opts, policyUUID, apiUUID)
	})
	return nil
}
