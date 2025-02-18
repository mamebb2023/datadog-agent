// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

// TODO: A lot of the code in this file is currently duplicated in taggertypes.
// We will need to move all the code in taggertype to this file and remove the taggertypes package.

// Package origindetection contains the types and functions used for Origin Detection.
package origindetection

import (
	"strconv"
	"strings"
)

// ProductOrigin is the origin of the product that sent the entity.
type ProductOrigin int

const (
	// ProductOriginDogStatsDLegacy is the ProductOrigin for DogStatsD in Legacy mode.
	// TODO: remove this when dogstatsd_origin_detection_unified is enabled by default
	ProductOriginDogStatsDLegacy ProductOrigin = iota
	// ProductOriginDogStatsD is the ProductOrigin for DogStatsD.
	ProductOriginDogStatsD ProductOrigin = iota
	// ProductOriginAPM is the ProductOrigin for APM.
	ProductOriginAPM ProductOrigin = iota

	// External Data Prefixes
	// These prefixes are used to build the External Data Environment Variable.

	// ExternalDataInitPrefix is the prefix for the Init flag in the External Data.
	ExternalDataInitPrefix = "it-"
	// ExternalDataContainerNamePrefix is the prefix for the Container Name in the External Data.
	ExternalDataContainerNamePrefix = "cn-"
	// ExternalDataPodUIDPrefix is the prefix for the Pod UID in the External Data.
	ExternalDataPodUIDPrefix = "pu-"
)

// OriginInfo contains the Origin Detection information.
type OriginInfo struct {
	LocalData     LocalData     // LocalData is the local data list.
	ExternalData  ExternalData  // ExternalData is the external data list.
	Cardinality   string        // Cardinality is the cardinality of the resolved origin.
	ProductOrigin ProductOrigin // ProductOrigin is the product that sent the origin information.
}

// LocalData that is generated by the client and sent to the Agent.
type LocalData struct {
	ProcessID   uint32 // ProcessID of the container process on the host.
	ContainerID string // ContainerID sent from the client.
	Inode       uint64 // Inode is the Cgroup inode of the container.
	PodUID      string // PodUID of the pod sent from the client.
}

// ExternalData generated by the Admission Controller and sent to the Agent.
type ExternalData struct {
	Init          bool   // Init is true if the container is an init container.
	ContainerName string // ContainerName is the name of the container as seen by the Admission Controller.
	PodUID        string // PodUID is the UID of the pod as seen by the Admission Controller.
}

// GenerateContainerIDFromExternalData generates a container ID from the external data.
type GenerateContainerIDFromExternalData func(externalData ExternalData) (string, error)

// ParseExternalData parses the external data string into an ExternalData struct.
func ParseExternalData(externalEnv string) (ExternalData, error) {
	if externalEnv == "" {
		return ExternalData{}, nil
	}
	var externalData ExternalData
	var parsingError error
	for _, item := range strings.Split(externalEnv, ",") {
		switch {
		case strings.HasPrefix(item, ExternalDataInitPrefix):
			externalData.Init, parsingError = strconv.ParseBool(item[len(ExternalDataInitPrefix):])
		case strings.HasPrefix(item, ExternalDataContainerNamePrefix):
			externalData.ContainerName = item[len(ExternalDataContainerNamePrefix):]
		case strings.HasPrefix(item, ExternalDataPodUIDPrefix):
			externalData.PodUID = item[len(ExternalDataPodUIDPrefix):]
		}
	}
	return externalData, parsingError
}
