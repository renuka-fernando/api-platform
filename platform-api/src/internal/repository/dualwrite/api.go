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

// associationTypeGateway is the v1 association_mappings.association_type for a gateway link.
// Only gateway associations are mirrored; dev_portal associations are dropped in v2.
const associationTypeGateway = "gateway"

// ---- APIRepository ----

type apiRepo struct {
	repository.APIRepository
	sink *Sink
}

// NewAPIRepo wraps a v1 APIRepository with v2 mirroring.
func NewAPIRepo(inner repository.APIRepository, sink *Sink) repository.APIRepository {
	return &apiRepo{APIRepository: inner, sink: sink}
}

func (d *apiRepo) CreateAPI(api *model.API) error {
	if err := d.APIRepository.CreateAPI(api); err != nil {
		return err
	}
	// One UpsertRestAPI writes the whole v2 footprint (artifacts + rest_apis).
	d.sink.mirrorUpsert("rest_api", "rest_apis", api.ID, api.OrganizationID, func(ex migrationcore.Execer) error {
		handle, row, err := readRestAPIRow(d.sink.v1, api.ID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertRestAPIV1(ex, handle, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *apiRepo) UpdateAPI(api *model.API) error {
	if err := d.APIRepository.UpdateAPI(api); err != nil {
		return err
	}
	d.sink.mirrorUpsert("rest_api", "rest_apis", api.ID, api.OrganizationID, func(ex migrationcore.Execer) error {
		handle, row, err := readRestAPIRow(d.sink.v1, api.ID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertRestAPIV1(ex, handle, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *apiRepo) DeleteAPI(apiUUID, orgUUID string) error {
	if err := d.APIRepository.DeleteAPI(apiUUID, orgUUID); err != nil {
		return err
	}
	// DeleteArtifact cascades the rest_apis row + all artifact-keyed children in v2.
	d.sink.mirrorDelete("rest_api", "rest_apis", apiUUID, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteArtifact(ex, d.sink.opts, apiUUID)
	})
	return nil
}

func (d *apiRepo) CreateAPIAssociation(association *model.APIAssociation) error {
	if err := d.APIRepository.CreateAPIAssociation(association); err != nil {
		return err
	}
	if association.AssociationType != associationTypeGateway {
		return nil // dev_portal associations are not mirrored (dropped in v2)
	}
	art, org, gw := association.ArtifactID, association.OrganizationID, association.ResourceID
	d.sink.mirrorUpsert("artifact_gateway_mapping", "artifact_gateway_mappings", org+"|"+art+"|"+gw, org, func(ex migrationcore.Execer) error {
		row, err := readArtifactGatewayMappingRow(d.sink.v1, art, org, gw)
		if err != nil {
			return err
		}
		return migrationcore.UpsertArtifactGatewayMappingV1(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *apiRepo) UpdateAPIAssociation(apiUUID, resourceID, associationType, orgUUID string) error {
	if err := d.APIRepository.UpdateAPIAssociation(apiUUID, resourceID, associationType, orgUUID); err != nil {
		return err
	}
	if associationType != associationTypeGateway {
		return nil
	}
	d.sink.mirrorUpsert("artifact_gateway_mapping", "artifact_gateway_mappings", orgUUID+"|"+apiUUID+"|"+resourceID, orgUUID, func(ex migrationcore.Execer) error {
		row, err := readArtifactGatewayMappingRow(d.sink.v1, apiUUID, orgUUID, resourceID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertArtifactGatewayMappingV1(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}
