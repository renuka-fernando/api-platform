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

// Artifact-backed type repositories (LLM template/provider/proxy, MCP, WebSub, WebBroker).
// Create mirrors by the minted model UUID; Update resolves the uuid from the (handle, org)
// natural key the v1 repos use; Delete resolves the uuid BEFORE the wrapped delete (the row
// is gone afterwards) and mirrors DeleteArtifact (cascading the type row + children in v2),
// except templates which have no artifact and use DeleteLLMProviderTemplate.

import (
	"platform-api/src/internal/constants"
	"platform-api/src/internal/model"
	"platform-api/src/internal/repository"

	"github.com/wso2/api-platform/platform-api/migrationcore"
)

// ---- LLMProviderTemplateRepository ----

type llmTemplateRepo struct {
	repository.LLMProviderTemplateRepository
	sink *Sink
}

// NewLLMProviderTemplateRepo wraps a v1 LLMProviderTemplateRepository with v2 mirroring.
func NewLLMProviderTemplateRepo(inner repository.LLMProviderTemplateRepository, sink *Sink) repository.LLMProviderTemplateRepository {
	return &llmTemplateRepo{LLMProviderTemplateRepository: inner, sink: sink}
}

func (d *llmTemplateRepo) Create(t *model.LLMProviderTemplate) error {
	if err := d.LLMProviderTemplateRepository.Create(t); err != nil {
		return err
	}
	d.sink.mirrorUpsert("llm_provider_template", "llm_provider_templates", t.UUID, t.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readLLMTemplateRow(d.sink.v1, t.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertLLMProviderTemplate(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *llmTemplateRepo) Update(t *model.LLMProviderTemplate) error {
	if err := d.LLMProviderTemplateRepository.Update(t); err != nil {
		return err
	}
	uuid, _ := resolveTemplateUUID(d.sink.v1, t.ID, t.OrganizationUUID)
	d.sink.mirrorResolvedUpsert("llm_provider_template", "llm_provider_templates", uuid, t.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readLLMTemplateRow(d.sink.v1, uuid)
		if err != nil {
			return err
		}
		return migrationcore.UpsertLLMProviderTemplate(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *llmTemplateRepo) Delete(templateID, orgUUID string) error {
	uuid, _ := resolveTemplateUUID(d.sink.v1, templateID, orgUUID) // resolve BEFORE the delete
	if err := d.LLMProviderTemplateRepository.Delete(templateID, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorResolvedDelete("llm_provider_template", "llm_provider_templates", uuid, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteLLMProviderTemplate(ex, d.sink.opts, uuid)
	})
	return nil
}

// ---- LLMProviderRepository ----

type llmProviderRepo struct {
	repository.LLMProviderRepository
	sink *Sink
}

// NewLLMProviderRepo wraps a v1 LLMProviderRepository with v2 mirroring.
func NewLLMProviderRepo(inner repository.LLMProviderRepository, sink *Sink) repository.LLMProviderRepository {
	return &llmProviderRepo{LLMProviderRepository: inner, sink: sink}
}

func (d *llmProviderRepo) Create(p *model.LLMProvider) error {
	if err := d.LLMProviderRepository.Create(p); err != nil {
		return err
	}
	d.sink.mirrorUpsert("llm_provider", "llm_providers", p.UUID, p.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readLLMProviderRow(d.sink.v1, p.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertLLMProvider(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *llmProviderRepo) Update(p *model.LLMProvider) error {
	if err := d.LLMProviderRepository.Update(p); err != nil {
		return err
	}
	uuid, _ := resolveArtifactUUID(d.sink.v1, p.ID, p.OrganizationUUID, constants.LLMProvider)
	d.sink.mirrorResolvedUpsert("llm_provider", "llm_providers", uuid, p.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readLLMProviderRow(d.sink.v1, uuid)
		if err != nil {
			return err
		}
		return migrationcore.UpsertLLMProvider(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *llmProviderRepo) Delete(providerID, orgUUID string) error {
	uuid, _ := resolveArtifactUUID(d.sink.v1, providerID, orgUUID, constants.LLMProvider)
	if err := d.LLMProviderRepository.Delete(providerID, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorResolvedDelete("llm_provider", "llm_providers", uuid, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteArtifact(ex, d.sink.opts, uuid)
	})
	return nil
}

// ---- LLMProxyRepository ----

type llmProxyRepo struct {
	repository.LLMProxyRepository
	sink *Sink
}

// NewLLMProxyRepo wraps a v1 LLMProxyRepository with v2 mirroring.
func NewLLMProxyRepo(inner repository.LLMProxyRepository, sink *Sink) repository.LLMProxyRepository {
	return &llmProxyRepo{LLMProxyRepository: inner, sink: sink}
}

func (d *llmProxyRepo) Create(p *model.LLMProxy) error {
	if err := d.LLMProxyRepository.Create(p); err != nil {
		return err
	}
	d.sink.mirrorUpsert("llm_proxy", "llm_proxies", p.UUID, p.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readLLMProxyRow(d.sink.v1, p.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertLLMProxy(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *llmProxyRepo) Update(p *model.LLMProxy) error {
	if err := d.LLMProxyRepository.Update(p); err != nil {
		return err
	}
	uuid, _ := resolveArtifactUUID(d.sink.v1, p.ID, p.OrganizationUUID, constants.LLMProxy)
	d.sink.mirrorResolvedUpsert("llm_proxy", "llm_proxies", uuid, p.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readLLMProxyRow(d.sink.v1, uuid)
		if err != nil {
			return err
		}
		return migrationcore.UpsertLLMProxy(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *llmProxyRepo) Delete(proxyID, orgUUID string) error {
	uuid, _ := resolveArtifactUUID(d.sink.v1, proxyID, orgUUID, constants.LLMProxy)
	if err := d.LLMProxyRepository.Delete(proxyID, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorResolvedDelete("llm_proxy", "llm_proxies", uuid, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteArtifact(ex, d.sink.opts, uuid)
	})
	return nil
}

// ---- MCPProxyRepository ----

type mcpProxyRepo struct {
	repository.MCPProxyRepository
	sink *Sink
}

// NewMCPProxyRepo wraps a v1 MCPProxyRepository with v2 mirroring.
func NewMCPProxyRepo(inner repository.MCPProxyRepository, sink *Sink) repository.MCPProxyRepository {
	return &mcpProxyRepo{MCPProxyRepository: inner, sink: sink}
}

func (d *mcpProxyRepo) Create(p *model.MCPProxy) error {
	if err := d.MCPProxyRepository.Create(p); err != nil {
		return err
	}
	d.sink.mirrorUpsert("mcp_proxy", "mcp_proxies", p.UUID, p.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readMCPProxyRow(d.sink.v1, p.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertMCPProxy(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *mcpProxyRepo) Update(p *model.MCPProxy) error {
	if err := d.MCPProxyRepository.Update(p); err != nil {
		return err
	}
	uuid, _ := resolveArtifactUUID(d.sink.v1, p.Handle, p.OrganizationUUID, constants.MCPProxy)
	d.sink.mirrorResolvedUpsert("mcp_proxy", "mcp_proxies", uuid, p.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readMCPProxyRow(d.sink.v1, uuid)
		if err != nil {
			return err
		}
		return migrationcore.UpsertMCPProxy(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *mcpProxyRepo) Delete(handle, orgUUID string) error {
	uuid, _ := resolveArtifactUUID(d.sink.v1, handle, orgUUID, constants.MCPProxy)
	if err := d.MCPProxyRepository.Delete(handle, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorResolvedDelete("mcp_proxy", "mcp_proxies", uuid, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteArtifact(ex, d.sink.opts, uuid)
	})
	return nil
}

// ---- WebSubAPIRepository ----

type webSubAPIRepo struct {
	repository.WebSubAPIRepository
	sink *Sink
}

// NewWebSubAPIRepo wraps a v1 WebSubAPIRepository with v2 mirroring. WebSub/WebBroker are
// experimental v2 plugin tables; mirroring them assumes the plugin schema is present in v2
// (dbmigrate applies it) — otherwise the mirror records a failure and v1 is unaffected.
func NewWebSubAPIRepo(inner repository.WebSubAPIRepository, sink *Sink) repository.WebSubAPIRepository {
	return &webSubAPIRepo{WebSubAPIRepository: inner, sink: sink}
}

func (d *webSubAPIRepo) Create(api *model.WebSubAPI) error {
	if err := d.WebSubAPIRepository.Create(api); err != nil {
		return err
	}
	d.sink.mirrorUpsert("websub_api", "websub_apis", api.UUID, api.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readWebSubRow(d.sink.v1, api.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertWebSubAPI(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *webSubAPIRepo) Update(api *model.WebSubAPI) error {
	if err := d.WebSubAPIRepository.Update(api); err != nil {
		return err
	}
	uuid, _ := resolveArtifactUUID(d.sink.v1, api.Handle, api.OrganizationUUID, constants.WebSubApi)
	d.sink.mirrorResolvedUpsert("websub_api", "websub_apis", uuid, api.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readWebSubRow(d.sink.v1, uuid)
		if err != nil {
			return err
		}
		return migrationcore.UpsertWebSubAPI(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *webSubAPIRepo) Delete(handle, orgUUID string) error {
	uuid, _ := resolveArtifactUUID(d.sink.v1, handle, orgUUID, constants.WebSubApi)
	if err := d.WebSubAPIRepository.Delete(handle, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorResolvedDelete("websub_api", "websub_apis", uuid, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteArtifact(ex, d.sink.opts, uuid)
	})
	return nil
}

// ---- WebBrokerAPIRepository ----

type webBrokerAPIRepo struct {
	repository.WebBrokerAPIRepository
	sink *Sink
}

// NewWebBrokerAPIRepo wraps a v1 WebBrokerAPIRepository with v2 mirroring.
func NewWebBrokerAPIRepo(inner repository.WebBrokerAPIRepository, sink *Sink) repository.WebBrokerAPIRepository {
	return &webBrokerAPIRepo{WebBrokerAPIRepository: inner, sink: sink}
}

func (d *webBrokerAPIRepo) Create(api *model.WebBrokerAPI) error {
	if err := d.WebBrokerAPIRepository.Create(api); err != nil {
		return err
	}
	d.sink.mirrorUpsert("webbroker_api", "webbroker_apis", api.UUID, api.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readWebBrokerRow(d.sink.v1, api.UUID)
		if err != nil {
			return err
		}
		return migrationcore.UpsertWebBrokerAPI(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *webBrokerAPIRepo) Update(api *model.WebBrokerAPI) error {
	if err := d.WebBrokerAPIRepository.Update(api); err != nil {
		return err
	}
	uuid, _ := resolveArtifactUUID(d.sink.v1, api.Handle, api.OrganizationUUID, constants.WebBrokerApi)
	d.sink.mirrorResolvedUpsert("webbroker_api", "webbroker_apis", uuid, api.OrganizationUUID, func(ex migrationcore.Execer) error {
		row, err := readWebBrokerRow(d.sink.v1, uuid)
		if err != nil {
			return err
		}
		return migrationcore.UpsertWebBrokerAPI(ex, row, d.sink.opts, d.sink.reporter)
	})
	return nil
}

func (d *webBrokerAPIRepo) Delete(handle, orgUUID string) error {
	uuid, _ := resolveArtifactUUID(d.sink.v1, handle, orgUUID, constants.WebBrokerApi)
	if err := d.WebBrokerAPIRepository.Delete(handle, orgUUID); err != nil {
		return err
	}
	d.sink.mirrorResolvedDelete("webbroker_api", "webbroker_apis", uuid, orgUUID, func(ex migrationcore.Execer) error {
		return migrationcore.DeleteArtifact(ex, d.sink.opts, uuid)
	})
	return nil
}
