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
	"time"

	"platform-api/src/internal/model"
	"platform-api/src/internal/repository"

	"github.com/wso2/api-platform/platform-api/migrationcore"
)

// ---- DeploymentRepository ----

type deploymentRepo struct {
	repository.DeploymentRepository
	sink *Sink
}

// NewDeploymentRepo wraps a v1 DeploymentRepository with v2 mirroring.
func NewDeploymentRepo(inner repository.DeploymentRepository, sink *Sink) repository.DeploymentRepository {
	return &deploymentRepo{DeploymentRepository: inner, sink: sink}
}

// mirrorDeploymentStatus mirrors the current deployment_status row for (artifact, org, gateway).
func (d *deploymentRepo) mirrorDeploymentStatus(artifactUUID, orgUUID, gatewayID string) {
	d.sink.mirrorUpsert("deployment_status", "deployment_status", orgUUID+"|"+artifactUUID+"|"+gatewayID, orgUUID, func(ex migrationcore.Execer) error {
		row, err := readDeploymentStatusRow(d.sink.v1, artifactUUID, orgUUID, gatewayID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertDeploymentStatus(ex, row, d.sink.opts, d.sink.reporter)
	})
}

func (d *deploymentRepo) CreateWithLimitEnforcement(deployment *model.Deployment, hardLimit int) error {
	art, gw, org := deployment.ArtifactID, deployment.GatewayID, deployment.OrganizationID
	// Snapshot the deployment id set BEFORE the call so we can learn which archived rows the
	// enforcement evicted (it deletes them internally and returns only error).
	before, beforeErr := readDeploymentIDs(d.sink.v1, art, gw, org)

	if err := d.DeploymentRepository.CreateWithLimitEnforcement(deployment, hardLimit); err != nil {
		return err
	}

	// Mirror evictions: ids present before but gone after (each independently reconcilable).
	if beforeErr == nil {
		if after, err := readDeploymentIDs(d.sink.v1, art, gw, org); err == nil {
			for id := range before {
				if !after[id] {
					evicted := id
					d.sink.mirrorDelete("deployment", "deployments", evicted, org, func(ex migrationcore.Execer) error {
						return migrationcore.DeleteDeployment(ex, d.sink.opts, evicted)
					})
				}
			}
		}
	}

	// Mirror the new immutable deployment, then its current status (FK: status → deployment,
	// so the deployment upsert must land first — these run as ordered synchronous mirrors).
	newID := deployment.DeploymentID
	d.sink.mirrorUpsert("deployment", "deployments", newID, org, func(ex migrationcore.Execer) error {
		row, err := readDeploymentRow(d.sink.v1, newID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertDeployment(ex, row, d.sink.opts, d.sink.reporter)
	})
	d.mirrorDeploymentStatus(art, org, gw)
	return nil
}

func (d *deploymentRepo) Delete(deploymentID, artifactUUID, orgUUID string) error {
	if err := d.DeploymentRepository.Delete(deploymentID, artifactUUID, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorDelete("deployment", "deployments", deploymentID, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteDeployment(ex, d.sink.opts, deploymentID)
	})
	return nil
}

func (d *deploymentRepo) SetCurrent(artifactUUID, orgUUID, gatewayID, deploymentID string, status model.DeploymentStatus) (time.Time, error) {
	updatedAt, err := d.DeploymentRepository.SetCurrent(artifactUUID, orgUUID, gatewayID, deploymentID, status)
	if err != nil {
		return updatedAt, err
	}
	d.mirrorDeploymentStatus(artifactUUID, orgUUID, gatewayID)
	return updatedAt, nil
}

func (d *deploymentRepo) SetCurrentWithDetails(artifactUUID, orgUUID, gatewayID, deploymentID string, status model.DeploymentStatus, statusDesired string, performedAt *time.Time, statusReason string) (time.Time, error) {
	updatedAt, err := d.DeploymentRepository.SetCurrentWithDetails(artifactUUID, orgUUID, gatewayID, deploymentID, status, statusDesired, performedAt, statusReason)
	if err != nil {
		return updatedAt, err
	}
	d.mirrorDeploymentStatus(artifactUUID, orgUUID, gatewayID)
	return updatedAt, nil
}

func (d *deploymentRepo) UpdateStatusWithPerformedAtGuard(artifactUUID, orgUUID, gatewayID string, newStatus model.DeploymentStatus, statusReason string, performedAt time.Time, requireCurrentStatus []model.DeploymentStatus) (int64, error) {
	rowsAffected, err := d.DeploymentRepository.UpdateStatusWithPerformedAtGuard(artifactUUID, orgUUID, gatewayID, newStatus, statusReason, performedAt, requireCurrentStatus)
	if err != nil {
		return rowsAffected, err
	}
	// A stale ack (guard mismatch) updates nothing — do not mirror a no-op.
	if rowsAffected > 0 {
		d.mirrorDeploymentStatus(artifactUUID, orgUUID, gatewayID)
	}
	return rowsAffected, nil
}

func (d *deploymentRepo) DeleteStatus(artifactUUID, orgUUID, gatewayID string) error {
	if err := d.DeploymentRepository.DeleteStatus(artifactUUID, orgUUID, gatewayID); err != nil {
		return err
	}
	d.sink.mirrorDelete("deployment_status", "deployment_status", orgUUID+"|"+artifactUUID+"|"+gatewayID, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteDeploymentStatus(ex, d.sink.opts, orgUUID, artifactUUID, gatewayID)
	})
	return nil
}
