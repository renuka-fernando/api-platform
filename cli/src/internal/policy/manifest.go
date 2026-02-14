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

package policy

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wso2/api-platform/cli/utils"
	"gopkg.in/yaml.v3"
)

// ParseBuildFile reads and parses a build file
func ParseBuildFile(buildFilePath string) (*BuildFile, error) {
	data, err := os.ReadFile(buildFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read build file: %w", err)
	}

	var buildFile BuildFile
	if err := yaml.Unmarshal(data, &buildFile); err != nil {
		return nil, fmt.Errorf("failed to parse build file: %w", err)
	}

	// Validate build file
	if err := validateBuildFile(&buildFile); err != nil {
		return nil, err
	}

	return &buildFile, nil
}

// ParseLockFile reads and parses a policy lock file
func ParseLockFile(lockPath string) (*PolicyLock, error) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read lock file: %w", err)
	}

	var lock PolicyLock
	if err := yaml.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("failed to parse lock file: %w", err)
	}

	return &lock, nil
}

// validateBuildFile validates the build file structure
func validateBuildFile(buildFile *BuildFile) error {
	if len(buildFile.Policies) == 0 {
		return fmt.Errorf("build file must contain at least one policy")
	}

	for i, policy := range buildFile.Policies {
		if policy.Name == "" {
			return fmt.Errorf("policy at index %d: name is required", i)
		}

		// Validate local policy when a filePath is provided
		if policy.FilePath != "" {
			// Resolve relative paths relative to build file directory
			policyPath := policy.FilePath
			if !filepath.IsAbs(policyPath) {
				// If relative, assume it's relative to working directory or build file location
				policyPath = filepath.Clean(policyPath)
			}

			// Check if path exists
			info, err := os.Stat(policyPath)
			if os.IsNotExist(err) {
				return fmt.Errorf("policy %s: path not found: %s", policy.Name, policy.FilePath)
			} else if err != nil {
				return fmt.Errorf("failed to access policy path %s: %w", policy.FilePath, err)
			}

			// Must be a directory containing policy-definition.yaml
			if !info.IsDir() {
				return fmt.Errorf("policy %s: path must be a directory containing policy-definition.yaml (zip files are not supported)", policy.Name)
			}

			if err := utils.ValidateLocalPolicyDir(policyPath, policy.Name); err != nil {
				return fmt.Errorf("policy %s: validation failed:\n%w\n\nLocal policies must:\n"+
					"  1. Be a directory containing policy-definition.yaml at the root\n"+
					"  2. Ensure 'name' matches the build file", policy.Name, err)
			}
		}
	}

	return nil
}

// SeparatePolicies separates build file policies into local and hub policies
func SeparatePolicies(buildFile *BuildFile) (local, hub []BuildFilePolicy) {
	for _, policy := range buildFile.Policies {
		if policy.IsLocal() {
			local = append(local, policy)
		} else {
			hub = append(hub, policy)
		}
	}
	return
}
