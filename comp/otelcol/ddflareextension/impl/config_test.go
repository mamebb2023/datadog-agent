// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2024-present Datadog, Inc.

// Package ddflareextensionimpl defines the OpenTelemetry Extension implementation.
package ddflareextensionimpl

import (
	"testing"

	configstore "github.com/DataDog/datadog-agent/comp/otelcol/configstore/impl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/config/confighttp"
	"go.opentelemetry.io/collector/confmap"
)

func getTestConfig(t *testing.T) *Config {
	conv, err := configstore.NewConfigStore()
	require.NoError(t, err)

	return &Config{
		ConfigStore: conv,
		HTTPConfig: &confighttp.ServerConfig{
			Endpoint: "localhost:0",
		},
	}
}

func TestValidate(t *testing.T) {
	cfg := getTestConfig(t)

	err := cfg.Validate()
	assert.NoError(t, err)

	cfg.HTTPConfig.Endpoint = ""
	err = cfg.Validate()
	assert.ErrorIs(t, err, errHTTPEndpointRequired)

	cfg.HTTPConfig = nil
	err = cfg.Validate()
	assert.ErrorIs(t, err, errHTTPEndpointRequired)
}

func TestUnmarshal(t *testing.T) {
	cfg := getTestConfig(t)

	endpoint := "localhost:1234"

	m := map[string]any{
		"endpoint": endpoint,
	}

	myConfMap := confmap.NewFromStringMap(m)

	err := cfg.Unmarshal(myConfMap)
	assert.NoError(t, err)

	err = cfg.Validate()
	assert.NoError(t, err)

	assert.Equal(t, endpoint, cfg.HTTPConfig.Endpoint)
}

func TestExtractors(t *testing.T) {

	endpoint := "localhost:1234"

	m := map[string]any{
		"endpoint": endpoint,
	}

	myConfMap := confmap.NewFromStringMap(m)

	for _, extractor := range supportedDebugExtensions {
		uri, err := extractor(myConfMap)
		assert.NoError(t, err)
		assert.Equal(t, endpoint, uri)
	}

	myConfMap = confmap.New()
	for _, extractor := range supportedDebugExtensions {
		_, err := extractor(myConfMap)
		assert.Error(t, err)
	}

}
