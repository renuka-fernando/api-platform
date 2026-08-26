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

// ---- APIKeyRepository ----

type apiKeyRepo struct {
	repository.APIKeyRepository
	sink *Sink
}

// NewAPIKeyRepo wraps a v1 APIKeyRepository with v2 mirroring.
func NewAPIKeyRepo(inner repository.APIKeyRepository, sink *Sink) repository.APIKeyRepository {
	return &apiKeyRepo{APIKeyRepository: inner, sink: sink}
}

// upsertAPIKey mirrors the FULL current api key (read back by uuid) — used by Create and by
// the partial mutators (Update, Revoke), so DO UPDATE never clobbers untouched v2 columns.
func (d *apiKeyRepo) upsertAPIKey(uuid string) {
	d.sink.mirrorResolvedUpsert("api_key", "api_keys", uuid, "", func(ex migrationcore.Execer) error {
		handle, row, err := readAPIKeyRow(d.sink.v1, uuid)
		if err != nil {
			return err
		}
		return migrationcore.UpsertAPIKeyV1(ex, handle, row, d.sink.opts, d.sink.reporter)
	})
}

func (d *apiKeyRepo) Create(key *model.APIKey) error {
	if err := d.APIKeyRepository.Create(key); err != nil {
		return err
	}
	d.upsertAPIKey(key.UUID)
	return nil
}

func (d *apiKeyRepo) Update(key *model.APIKey) error {
	if err := d.APIKeyRepository.Update(key); err != nil {
		return err
	}
	// Update is keyed by (artifact_uuid, name); resolve the uuid for a faithful read-back.
	uuid, _ := resolveAPIKeyUUID(d.sink.v1, key.ArtifactUUID, key.Name)
	d.upsertAPIKey(uuid)
	return nil
}

func (d *apiKeyRepo) Revoke(artifactUUID, name string) error {
	if err := d.APIKeyRepository.Revoke(artifactUUID, name); err != nil {
		return err
	}
	// Revoke is a SOFT update (status='revoked') — mirror the updated row via UpsertAPIKey.
	uuid, _ := resolveAPIKeyUUID(d.sink.v1, artifactUUID, name)
	d.upsertAPIKey(uuid)
	return nil
}

func (d *apiKeyRepo) Delete(artifactUUID, name string) error {
	uuid, _ := resolveAPIKeyUUID(d.sink.v1, artifactUUID, name) // resolve BEFORE the delete
	if err := d.APIKeyRepository.Delete(artifactUUID, name); err != nil {
		return err
	}
	d.sink.mirrorResolvedDelete("api_key", "api_keys", uuid, "", func(ex migrationcore.Execer) error {
		return migrationcore.DeleteAPIKey(ex, d.sink.opts, uuid)
	})
	return nil
}
