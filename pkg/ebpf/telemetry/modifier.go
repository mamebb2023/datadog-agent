// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

//go:build linux_bpf

package telemetry

import (
	"fmt"
	"slices"

	manager "github.com/DataDog/ebpf-manager"

	"github.com/DataDog/datadog-agent/pkg/ebpf/maps"
	"github.com/DataDog/datadog-agent/pkg/ebpf/names"
	"github.com/DataDog/datadog-agent/pkg/util/log"
)

const (
	// MapErrTelemetryMap is the map storing the map error telemetry
	mapErrTelemetryMapName string = "map_err_telemetry_map"
	// HelperErrTelemetryMap is the map storing the helper error telemetry
	helperErrTelemetryMapName string = "helper_err_telemetry_map"
)

// ErrorsTelemetryModifier is a modifier that sets up the manager to handle eBPF telemetry.
type ErrorsTelemetryModifier struct{}

// String returns the name of the modifier.
func (t *ErrorsTelemetryModifier) String() string {
	return "ErrorsTelemetryModifier"
}

// getMapNames returns the names of the maps in the manager.
func getMapNames(m *manager.Manager) ([]names.MapName, error) {
	var mapNames []names.MapName

	// we use map specs instead of iterating over the user defined `manager.Maps`
	// because the user defined list may not contain shared maps passed to the manager
	// via `manager.Options.MapEditors`. On the other hand, MapSpecs will include all maps
	// referenced in the ELF file associated with the manager
	specs, err := m.GetMapSpecs()
	if err != nil {
		return nil, err
	}

	for _, spec := range specs {
		mapNames = append(mapNames, names.NewMapNameFromMapSpec(spec))
	}

	return mapNames, nil
}

// BeforeInit sets up the manager to handle eBPF telemetry.
// It will patch the instructions of all the manager probes and `undefinedProbes` provided.
// Constants are replaced for map error and helper error keys with their respective values.
func (t *ErrorsTelemetryModifier) BeforeInit(m *manager.Manager, module names.ModuleName, opts *manager.Options) error {
	activateBPFTelemetry, err := ebpfTelemetrySupported()
	if err != nil {
		return err
	}
	if opts.MapSpecEditors == nil {
		opts.MapSpecEditors = make(map[string]manager.MapSpecEditor)
	}

	// add telemetry maps to list of maps, if not present
	if !slices.ContainsFunc(m.Maps, func(x *manager.Map) bool { return x.Name == mapErrTelemetryMapName }) {
		m.Maps = append(m.Maps, &manager.Map{Name: mapErrTelemetryMapName})
	}
	if !slices.ContainsFunc(m.Maps, func(x *manager.Map) bool { return x.Name == helperErrTelemetryMapName }) {
		m.Maps = append(m.Maps, &manager.Map{Name: helperErrTelemetryMapName})
	}

	// set a small max entries value if telemetry is not supported. We have to load the maps because the eBPF code
	// references them even when we cannot track the telemetry.
	opts.MapSpecEditors[mapErrTelemetryMapName] = manager.MapSpecEditor{
		MaxEntries: uint32(1),
		EditorFlag: manager.EditMaxEntries,
	}
	opts.MapSpecEditors[helperErrTelemetryMapName] = manager.MapSpecEditor{
		MaxEntries: uint32(1),
		EditorFlag: manager.EditMaxEntries,
	}

	if activateBPFTelemetry {
		ebpfMaps, err := m.GetMapSpecs()
		if err != nil {
			return fmt.Errorf("failed to get map specs from manager: %w", err)
		}

		ebpfPrograms, err := m.GetProgramSpecs()
		if err != nil {
			return fmt.Errorf("failed to get program specs from manager: %w", err)
		}

		opts.MapSpecEditors[mapErrTelemetryMapName] = manager.MapSpecEditor{
			MaxEntries: uint32(len(ebpfMaps)),
			EditorFlag: manager.EditMaxEntries,
		}
		log.Tracef("module %s maps %d", module.Name(), opts.MapSpecEditors[mapErrTelemetryMapName].MaxEntries)

		opts.MapSpecEditors[helperErrTelemetryMapName] = manager.MapSpecEditor{
			MaxEntries: uint32(len(ebpfPrograms)),
			EditorFlag: manager.EditMaxEntries,
		}
		log.Tracef("module %s probes %d", module.Name(), opts.MapSpecEditors[helperErrTelemetryMapName].MaxEntries)

		mapNames, err := getMapNames(m)
		if err != nil {
			return err
		}

		h := keyHash()
		for _, mapName := range mapNames {
			opts.ConstantEditors = append(opts.ConstantEditors, manager.ConstantEditor{
				Name:  mapName.Name() + "_telemetry_key",
				Value: eBPFMapErrorKey(h, mapTelemetryKey(mapName, module)),
			})
		}

	}

	m.InstructionPatchers = append(m.InstructionPatchers, func(m *manager.Manager) error {
		specs, err := m.GetProgramSpecs()
		if err != nil {
			return err
		}
		return patchEBPFTelemetry(specs, activateBPFTelemetry, module, errorsTelemetry)
	})

	return nil
}

// getErrMaps returns the mapErrMap and helperErrMap from the manager.
func getErrMaps(m *manager.Manager) (mapErrMap *maps.GenericMap[uint64, mapErrTelemetry], helperErrMap *maps.GenericMap[uint64, helperErrTelemetry], err error) {
	mapErrMap, err = maps.GetMap[uint64, mapErrTelemetry](m, mapErrTelemetryMapName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get generic map %s: %w", mapErrTelemetryMapName, err)
	}

	helperErrMap, err = maps.GetMap[uint64, helperErrTelemetry](m, helperErrTelemetryMapName)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get generic map %s: %w", helperErrTelemetryMapName, err)
	}

	return mapErrMap, helperErrMap, nil
}

// AfterInit pre-populates the telemetry maps with entries corresponding to the ebpf program of the manager.
func (t *ErrorsTelemetryModifier) AfterInit(m *manager.Manager, module names.ModuleName, _ *manager.Options) error {
	if errorsTelemetry == nil {
		return nil
	}

	genericMapErrMap, genericHelperErrMap, err := getErrMaps(m)
	if err != nil {
		return err
	}

	mapNames, err := getMapNames(m)
	if err != nil {
		return err
	}

	if err := errorsTelemetry.fill(mapNames, module, genericMapErrMap, genericHelperErrMap); err != nil {
		return err
	}

	return nil
}

// BeforeStop stops the perf collector from telemetry and removes the modules from the telemetry maps.
func (t *ErrorsTelemetryModifier) BeforeStop(m *manager.Manager, module names.ModuleName) error {
	if errorsTelemetry == nil {
		return nil
	}

	genericMapErrMap, genericHelperErrMap, err := getErrMaps(m)
	if err != nil {
		return err
	}

	mapNames, err := getMapNames(m)
	if err != nil {
		return err
	}

	if err := errorsTelemetry.cleanup(mapNames, module, genericMapErrMap, genericHelperErrMap); err != nil {
		return err
	}

	return nil
}
