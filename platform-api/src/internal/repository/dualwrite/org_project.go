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

// Each decorator embeds the real v1 repository interface, so every read method delegates
// unchanged (reads are always served from v1). Only the mutating methods are overridden:
// they call the wrapped v1 repo FIRST and, on success, mirror the resulting state into v2.

// ---- OrganizationRepository ----

type orgRepo struct {
	repository.OrganizationRepository
	sink *Sink
}

// NewOrganizationRepo wraps a v1 OrganizationRepository with v2 mirroring.
func NewOrganizationRepo(inner repository.OrganizationRepository, sink *Sink) repository.OrganizationRepository {
	return &orgRepo{OrganizationRepository: inner, sink: sink}
}

func (d *orgRepo) CreateOrganization(org *model.Organization) error {
	if err := d.OrganizationRepository.CreateOrganization(org); err != nil {
		return err
	}
	d.sink.mirrorUpsert("organization", "organizations", org.ID, org.ID, func(ex migrationcore.Execer) error {
		row, err := readOrganizationRow(d.sink.v1, org.ID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertOrganization(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *orgRepo) UpdateOrganization(org *model.Organization) error {
	if err := d.OrganizationRepository.UpdateOrganization(org); err != nil {
		return err
	}
	d.sink.mirrorUpsert("organization", "organizations", org.ID, org.ID, func(ex migrationcore.Execer) error {
		row, err := readOrganizationRow(d.sink.v1, org.ID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertOrganization(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *orgRepo) DeleteOrganization(orgID string) error {
	if err := d.OrganizationRepository.DeleteOrganization(orgID); err != nil {
		return err
	}
	d.sink.mirrorDelete("organization", "organizations", orgID, orgID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteOrganization(ex, orgID)
	})
	return nil
}

// ---- ProjectRepository ----

type projectRepo struct {
	repository.ProjectRepository
	sink *Sink
}

// NewProjectRepo wraps a v1 ProjectRepository with v2 mirroring.
func NewProjectRepo(inner repository.ProjectRepository, sink *Sink) repository.ProjectRepository {
	return &projectRepo{ProjectRepository: inner, sink: sink}
}

func (d *projectRepo) CreateProject(project *model.Project) error {
	if err := d.ProjectRepository.CreateProject(project); err != nil {
		return err
	}
	d.sink.mirrorUpsert("project", "projects", project.ID, project.OrganizationID, func(ex migrationcore.Execer) error {
		row, err := readProjectRow(d.sink.v1, project.ID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertProject(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *projectRepo) UpdateProject(project *model.Project) error {
	if err := d.ProjectRepository.UpdateProject(project); err != nil {
		return err
	}
	d.sink.mirrorUpsert("project", "projects", project.ID, project.OrganizationID, func(ex migrationcore.Execer) error {
		row, err := readProjectRow(d.sink.v1, project.ID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertProject(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *projectRepo) DeleteProject(projectID string) error {
	if err := d.ProjectRepository.DeleteProject(projectID); err != nil {
		return err
	}
	// v1 DeleteProject takes only the id; org is informational in the failure row.
	d.sink.mirrorDelete("project", "projects", projectID, "", func(ex migrationcore.Execer) error {
		return migrationcore.DeleteProject(ex, projectID)
	})
	return nil
}
