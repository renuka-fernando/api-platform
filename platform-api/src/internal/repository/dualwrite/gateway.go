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

// ---- GatewayRepository ----

type gatewayRepo struct {
	repository.GatewayRepository
	sink *Sink
}

// NewGatewayRepo wraps a v1 GatewayRepository with v2 mirroring.
func NewGatewayRepo(inner repository.GatewayRepository, sink *Sink) repository.GatewayRepository {
	return &gatewayRepo{GatewayRepository: inner, sink: sink}
}

// upsertGateway mirrors the FULL current gateway state (read back from v1). Used by Create
// and by every partial mutator (UpdateGateway / UpdateActiveStatus / UpdateGatewayManifest),
// so the idempotent DO UPDATE never clobbers v2 columns the partial write did not touch.
// §8.3: UpsertGateway replaces a superseded gateway_endpoints row when the vhost changed.
func (d *gatewayRepo) upsertGateway(gatewayID, org string) {
	d.sink.mirrorUpsert("gateway", "gateways", gatewayID, org, func(ex migrationcore.Execer) error {
		row, err := readGatewayRow(d.sink.v1, gatewayID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertGateway(ex, row, d.sink.opts, d.sink.reporter)
	})
}

func (d *gatewayRepo) Create(gateway *model.Gateway) error {
	if err := d.GatewayRepository.Create(gateway); err != nil {
		return err
	}
	d.upsertGateway(gateway.ID, gateway.OrganizationID)
	return nil
}

func (d *gatewayRepo) UpdateGateway(gateway *model.Gateway) error {
	if err := d.GatewayRepository.UpdateGateway(gateway); err != nil {
		return err
	}
	d.upsertGateway(gateway.ID, gateway.OrganizationID)
	return nil
}

func (d *gatewayRepo) UpdateActiveStatus(gatewayID string, isActive bool) error {
	if err := d.GatewayRepository.UpdateActiveStatus(gatewayID, isActive); err != nil {
		return err
	}
	d.upsertGateway(gatewayID, "")
	return nil
}

func (d *gatewayRepo) UpdateGatewayManifest(gatewayID string, manifest []byte) error {
	if err := d.GatewayRepository.UpdateGatewayManifest(gatewayID, manifest); err != nil {
		return err
	}
	d.upsertGateway(gatewayID, "")
	return nil
}

func (d *gatewayRepo) Delete(gatewayID, organizationID string) error {
	if err := d.GatewayRepository.Delete(gatewayID, organizationID); err != nil {
		return err
	}
	// DeleteGateway cascades gateway_endpoints/gateway_tokens/deployments/deployment_status/
	// artifact_gateway_mappings in v2 (matching v1's cleanup).
	d.sink.mirrorDelete("gateway", "gateways", gatewayID, organizationID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteGateway(ex, gatewayID)
	})
	return nil
}

func (d *gatewayRepo) CreateToken(token *model.GatewayToken) error {
	if err := d.GatewayRepository.CreateToken(token); err != nil {
		return err
	}
	d.sink.mirrorUpsert("gateway_token", "gateway_tokens", token.ID, "", func(ex migrationcore.Execer) error {
		row, err := readGatewayTokenRow(d.sink.v1, token.ID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertGatewayToken(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *gatewayRepo) RevokeToken(tokenID string) error {
	if err := d.GatewayRepository.RevokeToken(tokenID); err != nil {
		return err
	}
	// RevokeToken is a SOFT update (status='revoked', revoked_at set) — mirror the updated
	// row via UpsertGatewayToken, NEVER DeleteGatewayToken.
	d.sink.mirrorUpsert("gateway_token", "gateway_tokens", tokenID, "", func(ex migrationcore.Execer) error {
		row, err := readGatewayTokenRow(d.sink.v1, tokenID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertGatewayToken(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}
