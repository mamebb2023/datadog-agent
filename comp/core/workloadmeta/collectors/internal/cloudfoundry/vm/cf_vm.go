// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

// Package vm provides a workloadmeta collector for CloudFoundry VM
package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go.uber.org/fx"

	workloadmeta "github.com/DataDog/datadog-agent/comp/core/workloadmeta/def"
	"github.com/DataDog/datadog-agent/pkg/config"
	"github.com/DataDog/datadog-agent/pkg/config/env"
	"github.com/DataDog/datadog-agent/pkg/errors"
	"github.com/DataDog/datadog-agent/pkg/util/cloudproviders/cloudfoundry"
	"github.com/DataDog/datadog-agent/pkg/util/clusteragent"
	ddjson "github.com/DataDog/datadog-agent/pkg/util/json"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

const (
	collectorID   = "cloudfoundry-vm"
	componentName = "workloadmeta-cloudfoundry-vm"
)

type collector struct {
	id      string
	store   workloadmeta.Component
	seen    map[workloadmeta.EntityID]struct{}
	catalog workloadmeta.AgentType

	gardenUtil cloudfoundry.GardenUtilInterface
	nodeName   string

	dcaClient  clusteragent.DCAClientInterface
	dcaEnabled bool
}

// NewCollector instantiates a CollectorProvider which can provide a CF container collector
func NewCollector() (workloadmeta.CollectorProvider, error) {
	return workloadmeta.CollectorProvider{
		Collector: &collector{
			id:      collectorID,
			seen:    make(map[workloadmeta.EntityID]struct{}),
			catalog: workloadmeta.NodeAgent | workloadmeta.ProcessAgent,
		},
	}, nil
}

// GetFxOptions returns the FX framework options for the collector
func GetFxOptions() fx.Option {
	return fx.Provide(NewCollector)
}

func (c *collector) Start(_ context.Context, store workloadmeta.Component) error {
	if !env.IsFeaturePresent(env.CloudFoundry) {
		return errors.NewDisabled(componentName, "Agent is not running on CloudFoundry")
	}

	c.store = store

	// Detect if we're on a compute VM by trying to connect to the local garden API
	var err error
	c.gardenUtil, err = cloudfoundry.GetGardenUtil()
	if err != nil {
		return err
	}

	c.nodeName = config.Datadog().GetString("bosh_id")

	// Check for Cluster Agent availability (will be retried at each pull)
	c.dcaEnabled = config.Datadog().GetBool("cluster_agent.enabled")
	c.dcaClient = c.getDCAClient()

	return nil
}

func (c *collector) Pull(_ context.Context) error {
	containers, err := c.gardenUtil.ListContainers()
	if err != nil {
		return err
	}

	handles := cloudfoundry.ContainersToHandles(containers)
	containersInfo, err := c.gardenUtil.GetContainersInfo(handles)
	if err != nil {
		return err
	}

	containersMetrics, err := c.gardenUtil.GetContainersMetrics(handles)
	if err != nil {
		return err
	}

	var allContainersTags map[string][]string
	if dcaClient := c.getDCAClient(); dcaClient != nil {
		allContainersTags, err = c.dcaClient.GetCFAppsMetadataForNode(c.nodeName)
		if err != nil {
			log.Debugf("Unable to fetch CF tags from cluster agent, CF tags will be missing, err: %v", err)
		}
	}

	currentTime := time.Now()
	events := make([]workloadmeta.CollectorEvent, 0, len(handles))
	seen := make(map[workloadmeta.EntityID]struct{})

	for id, containerInfo := range containersInfo {
		if containerInfo.Err != nil {
			log.Debugf("Failed to retrieve info for garden container: %s, err: %v", id, containerInfo.Err.Err)
			continue
		}

		// Not checking if present as only one field is used later on
		containerMetrics := containersMetrics[id]

		entityID := workloadmeta.EntityID{
			Kind: workloadmeta.KindContainer,
			ID:   id,
		}

		seen[entityID] = struct{}{}

		// Create container based on containerInfo + containerMetrics
		containerEntity := &workloadmeta.Container{
			EntityID: entityID,
			EntityMeta: workloadmeta.EntityMeta{
				Name: id,
			},
			Runtime: workloadmeta.ContainerRuntimeGarden,
			State: workloadmeta.ContainerState{
				StartedAt: currentTime.Add(-containerMetrics.Metrics.Age),
				CreatedAt: currentTime.Add(-containerMetrics.Metrics.Age),
			},
		}

		// Fill tags
		if tags, found := allContainersTags[id]; found {
			containerEntity.CollectorTags = tags
		} else {
			// Parse tags from garden.Container.Properties["logs_config"]["tags"]["app_name"]
			if logConfigJSON, found := containerInfo.Info.Properties["log_config"]; found {
				var config map[string]interface{}
				if err := json.Unmarshal([]byte(logConfigJSON), &config); err == nil {
					if appName := ddjson.GetNestedValue(config, "tags", "app_name"); appName != nil {
						containerEntity.CollectorTags = []string{
							fmt.Sprintf("%s:%s", cloudfoundry.ContainerNameTagKey, appName.(string)),
							fmt.Sprintf("%s:%s", cloudfoundry.AppInstanceGUIDTagKey, id),
						}
					}
				}
			}
		}

		// Default tags if none were found
		if len(containerEntity.CollectorTags) == 0 {
			containerEntity.CollectorTags = []string{
				fmt.Sprintf("%s:%s", cloudfoundry.ContainerNameTagKey, id),
				fmt.Sprintf("%s:%s", cloudfoundry.AppInstanceGUIDTagKey, id),
			}
		}

		// Store container state
		if containerInfo.Info.State == "active" {
			containerEntity.State.Running = true
			containerEntity.State.Status = workloadmeta.ContainerStatusRunning
		} else {
			containerEntity.State.Running = false
			containerEntity.State.Status = workloadmeta.ContainerStatusStopped
		}

		// Store IP Adresses + Ports
		containerEntity.NetworkIPs = map[string]string{
			"": containerInfo.Info.ExternalIP,
		}

		for _, port := range containerInfo.Info.MappedPorts {
			containerEntity.Ports = append(containerEntity.Ports, workloadmeta.ContainerPort{
				Port:     int(port.HostPort),
				Protocol: "tcp",
			})
		}

		events = append(events, workloadmeta.CollectorEvent{
			Type:   workloadmeta.EventTypeSet,
			Source: workloadmeta.SourceClusterOrchestrator,
			Entity: containerEntity,
		})
	}

	for seenID := range c.seen {
		if _, ok := seen[seenID]; ok {
			continue
		}

		events = append(events, workloadmeta.CollectorEvent{
			Type:   workloadmeta.EventTypeUnset,
			Source: workloadmeta.SourceClusterOrchestrator,
			Entity: &workloadmeta.Container{
				EntityID: seenID,
			},
		})
	}

	c.seen = seen

	c.store.Notify(events)

	return nil
}

func (c *collector) getDCAClient() clusteragent.DCAClientInterface {
	if !c.dcaEnabled {
		return nil
	}

	if c.dcaClient != nil {
		return c.dcaClient
	}

	var err error
	c.dcaClient, err = clusteragent.GetClusterAgentClient()
	if err != nil {
		log.Debugf("Could not initialise the communication with the cluster agent, PCF tags may be missing, err: %v", err)
		return nil
	}

	return c.dcaClient
}

func (c *collector) GetID() string {
	return c.id
}

func (c *collector) GetTargetCatalog() workloadmeta.AgentType {
	return c.catalog
}
