// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package containers

import (
	"strings"
	"sync"

	pkgconfigsetup "github.com/DataDog/datadog-agent/pkg/config/setup"
)

var (
	defaultEnvVarsIncludeList = []string{
		"CHRONOS_JOB_NAME",
		"CHRONOS_JOB_OWNER",
		"DD_ENV",
		"DD_GIT_COMMIT_SHA",
		"DD_GIT_REPOSITORY_URL",
		"DD_SERVICE",
		"DD_VERSION",
		"DOCKER_DD_AGENT", // included to be able to detect agent containers
		"ECS_CONTAINER_METADATA_URI",
		"ECS_CONTAINER_METADATA_URI_V4",
		"MESOS_TASK_ID",
		"NOMAD_DC",
		"NOMAD_GROUP_NAME",
		"NOMAD_JOB_NAME",
		"NOMAD_NAMESPACE",
		"NOMAD_TASK_NAME",
		"OTEL_RESOURCE_ATTRIBUTES",
		"OTEL_SERVICE_NAME",
		"TEST_ENV", // Included to ease unit tests without requiring a mock
	}

	envFilterOnce       sync.Once
	envFilterFromConfig EnvFilter
)

// EnvVarFilterFromConfig returns an EnvFilter based on the options present in the config
func EnvVarFilterFromConfig() EnvFilter {
	envFilterOnce.Do(func() {
		configEnvVars := make([]string, 0)
		dockerEnvs := pkgconfigsetup.Datadog().GetStringMapString("docker_env_as_tags")
		for envName := range dockerEnvs {
			configEnvVars = append(configEnvVars, envName)
		}

		containerEnvs := pkgconfigsetup.Datadog().GetStringMapString("container_env_as_tags")
		for envName := range containerEnvs {
			configEnvVars = append(configEnvVars, envName)
		}

		envFilterFromConfig = newEnvFilter(configEnvVars)
	})

	return envFilterFromConfig
}

// EnvFilter defines a filter for environment variables
type EnvFilter struct {
	includeVars map[string]struct{}
}

func newEnvFilter(includeVars []string) EnvFilter {
	filter := EnvFilter{
		includeVars: make(map[string]struct{}),
	}

	for _, varName := range defaultEnvVarsIncludeList {
		filter.includeVars[strings.ToUpper(varName)] = struct{}{}
	}

	for _, varName := range includeVars {
		filter.includeVars[strings.ToUpper(varName)] = struct{}{}
	}

	return filter
}

// IsIncluded returns whether the given env variable name is included
func (f EnvFilter) IsIncluded(envVarName string) bool {
	_, found := f.includeVars[strings.ToUpper(envVarName)]
	return found
}
