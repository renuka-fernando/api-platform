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

// ---- SubscriptionRepository ----

type subscriptionRepo struct {
	repository.SubscriptionRepository
	sink *Sink
}

// NewSubscriptionRepo wraps a v1 SubscriptionRepository with v2 mirroring.
func NewSubscriptionRepo(inner repository.SubscriptionRepository, sink *Sink) repository.SubscriptionRepository {
	return &subscriptionRepo{SubscriptionRepository: inner, sink: sink}
}

func (d *subscriptionRepo) Create(sub *model.Subscription) error {
	if err := d.SubscriptionRepository.Create(sub); err != nil {
		return err
	}
	// Read back the ENCRYPTED token + hash from v1 (the model carries only the decrypted
	// token and no hash), so the mirror stores exactly what the batch would.
	d.sink.mirrorUpsert("subscription", "subscriptions", sub.UUID, sub.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readSubscriptionRow(d.sink.v1, sub.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertSubscription(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *subscriptionRepo) Update(sub *model.Subscription) error {
	if err := d.SubscriptionRepository.Update(sub); err != nil {
		return err
	}
	d.sink.mirrorUpsert("subscription", "subscriptions", sub.UUID, sub.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readSubscriptionRow(d.sink.v1, sub.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertSubscription(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *subscriptionRepo) Delete(subscriptionID, orgUUID string) error {
	if err := d.SubscriptionRepository.Delete(subscriptionID, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorDelete("subscription", "subscriptions", subscriptionID, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteSubscription(ex, d.sink.opts, subscriptionID)
	})
	return nil
}

// ---- SubscriptionPlanRepository ----

type subscriptionPlanRepo struct {
	repository.SubscriptionPlanRepository
	sink *Sink
}

// NewSubscriptionPlanRepo wraps a v1 SubscriptionPlanRepository with v2 mirroring.
func NewSubscriptionPlanRepo(inner repository.SubscriptionPlanRepository, sink *Sink) repository.SubscriptionPlanRepository {
	return &subscriptionPlanRepo{SubscriptionPlanRepository: inner, sink: sink}
}

func (d *subscriptionPlanRepo) Create(plan *model.SubscriptionPlan) error {
	if err := d.SubscriptionPlanRepository.Create(plan); err != nil {
		return err
	}
	d.sink.mirrorUpsert("subscription_plan", "subscription_plans", plan.UUID, plan.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readSubscriptionPlanRow(d.sink.v1, plan.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertSubscriptionPlan(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *subscriptionPlanRepo) Update(plan *model.SubscriptionPlan) error {
	if err := d.SubscriptionPlanRepository.Update(plan); err != nil {
		return err
	}
	// §8.3: a throttle-unit change moves the limit's natural key; UpsertSubscriptionPlan
	// replaces the superseded subscription_plan_limits row under InsertOnly:false.
	d.sink.mirrorUpsert("subscription_plan", "subscription_plans", plan.UUID, plan.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readSubscriptionPlanRow(d.sink.v1, plan.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertSubscriptionPlan(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *subscriptionPlanRepo) Delete(planID, orgUUID string) error {
	if err := d.SubscriptionPlanRepository.Delete(planID, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorDelete("subscription_plan", "subscription_plans", planID, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteSubscriptionPlan(ex, d.sink.opts, planID)
	})
	return nil
}
