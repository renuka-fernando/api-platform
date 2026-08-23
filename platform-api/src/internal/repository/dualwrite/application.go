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

// ---- ApplicationRepository ----

type applicationRepo struct {
	repository.ApplicationRepository
	sink *Sink
}

// NewApplicationRepo wraps a v1 ApplicationRepository with v2 mirroring.
func NewApplicationRepo(inner repository.ApplicationRepository, sink *Sink) repository.ApplicationRepository {
	return &applicationRepo{ApplicationRepository: inner, sink: sink}
}

func (d *applicationRepo) CreateApplication(app *model.Application) error {
	if err := d.ApplicationRepository.CreateApplication(app); err != nil {
		return err
	}
	d.sink.mirrorUpsert("application", "applications", app.UUID, app.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readApplicationRow(d.sink.v1, app.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertApplication(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *applicationRepo) UpdateApplication(app *model.Application) error {
	if err := d.ApplicationRepository.UpdateApplication(app); err != nil {
		return err
	}
	d.sink.mirrorUpsert("application", "applications", app.UUID, app.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readApplicationRow(d.sink.v1, app.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertApplication(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *applicationRepo) DeleteApplication(appID, orgID string) error {
	if err := d.ApplicationRepository.DeleteApplication(appID, orgID); err != nil {
		return err
	}
	d.sink.mirrorDelete("application", "applications", appID, orgID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteApplication(ex, appID)
	})
	return nil
}

func (d *applicationRepo) AddApplicationAPIKeys(applicationUUID string, apiKeyIDs []string) error {
	if err := d.ApplicationRepository.AddApplicationAPIKeys(applicationUUID, apiKeyIDs); err != nil {
		return err
	}
	// Mirror each requested mapping; the upsert is idempotent (a mapping v1 skipped as a
	// duplicate is a no-op in v2 too), and the read-back picks up the stored created_at.
	for _, apiKeyID := range apiKeyIDs {
		id := apiKeyID
		d.sink.mirrorUpsert("application_api_key_mapping", "application_api_key_mappings", applicationUUID+"|"+id, "", func(ex migrationcore.Execer) error {
			row, err := readAppAPIKeyMappingRow(d.sink.v1, applicationUUID, id)
			if err != nil {
				return err
			}
			return migrationcore.UpsertApplicationAPIKeyMapping(ex, row, d.sink.opts, d.sink.reporter)
		})
	}
	return nil
}

func (d *applicationRepo) AddApplicationAssociations(applicationUUID string, targetUUIDs []string) error {
	if err := d.ApplicationRepository.AddApplicationAssociations(applicationUUID, targetUUIDs); err != nil {
		return err
	}
	for _, targetUUID := range targetUUIDs {
		target := targetUUID
		d.sink.mirrorUpsert("application_artifact_mapping", "application_artifact_mappings", applicationUUID+"|"+target, "", func(ex migrationcore.Execer) error {
			row, err := readAppArtifactMappingRow(d.sink.v1, applicationUUID, target)
			if err != nil {
				return err
			}
			return migrationcore.UpsertApplicationArtifactMapping(ex, row, d.sink.opts, d.sink.reporter)
		})
	}
	return nil
}

func (d *applicationRepo) RemoveApplicationAPIKey(applicationUUID, apiKeyID string) error {
	if err := d.ApplicationRepository.RemoveApplicationAPIKey(applicationUUID, apiKeyID); err != nil {
		return err
	}
	d.sink.mirrorDelete("application_api_key_mapping", "application_api_key_mappings", applicationUUID+"|"+apiKeyID, "", func(ex migrationcore.Execer) error {
		return migrationcore.DeleteApplicationAPIKeyMapping(ex, applicationUUID, apiKeyID)
	})
	return nil
}

func (d *applicationRepo) RemoveApplicationAssociation(applicationUUID, targetUUID string) error {
	if err := d.ApplicationRepository.RemoveApplicationAssociation(applicationUUID, targetUUID); err != nil {
		return err
	}
	d.sink.mirrorDelete("application_artifact_mapping", "application_artifact_mappings", applicationUUID+"|"+targetUUID, "", func(ex migrationcore.Execer) error {
		return migrationcore.DeleteApplicationArtifactMapping(ex, applicationUUID, targetUUID)
	})
	return nil
}
