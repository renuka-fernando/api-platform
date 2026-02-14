/*
 * Copyright (c) 2025, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package gateway

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// BuildFile represents the build file structure
type BuildFile struct {
	Version           string   `yaml:"version"`
	VersionResolution string   `yaml:"versionResolution,omitempty"`
	Policies          []Policy `yaml:"policies"`
}

// Policy represents a single policy in the build file
type Policy struct {
	Name              string `yaml:"name"`
	Version           string `yaml:"version"`
	VersionResolution string `yaml:"versionResolution,omitempty"`
	FilePath          string `yaml:"filePath,omitempty"`
}

// ValidateBuildFile validates the build file structure
func ValidateBuildFile(buildFile *BuildFile) error {

	// Validate policies array
	if len(buildFile.Policies) == 0 {
		return fmt.Errorf("'policies' array is required and must not be empty")
	}

	// Validate each policy
	for i, policy := range buildFile.Policies {
		if err := validatePolicy(&policy, i); err != nil {
			return err
		}
	}

	return nil
}

// validatePolicy validates a single policy entry
func validatePolicy(policy *Policy, index int) error {
	// Validate name (required)
	if policy.Name == "" {
		return fmt.Errorf("policy[%d]: 'name' field is required", index)
	}

	// Validate filePath if provided (check if file exists)
	if policy.FilePath != "" {
		if _, err := os.Stat(policy.FilePath); os.IsNotExist(err) {
			return fmt.Errorf("policy[%d] (%s): file path does not exist: %s", index, policy.Name, policy.FilePath)
		}
	}

	return nil
}

// LoadBuildFile loads and validates a build file
func LoadBuildFile(filePath string) (*BuildFile, error) {
	// Read the file
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read build file: %w", err)
	}

	// Parse YAML
	var buildFile BuildFile
	if err := yaml.Unmarshal(content, &buildFile); err != nil {
		return nil, fmt.Errorf("failed to parse build file YAML: %w", err)
	}

	// Validate the build file
	if err := ValidateBuildFile(&buildFile); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return &buildFile, nil
}
