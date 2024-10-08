// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package decoder

import (
	"time"

	"github.com/DataDog/datadog-agent/pkg/config"
	automultilinedetection "github.com/DataDog/datadog-agent/pkg/logs/internal/decoder/auto_multiline_detection"
	"github.com/DataDog/datadog-agent/pkg/logs/message"
	status "github.com/DataDog/datadog-agent/pkg/logs/status/utils"
)

// AutoMultilineHandler aggreagates multiline logs.
type AutoMultilineHandler struct {
	labeler    *automultilinedetection.Labeler
	aggregator *automultilinedetection.Aggregator
}

// NewAutoMultilineHandler creates a new auto multiline handler.
func NewAutoMultilineHandler(outputFn func(m *message.Message), maxContentSize int, flushTimeout time.Duration, tailerInfo *status.InfoRegistry) *AutoMultilineHandler {

	// Order is important
	heuristics := []automultilinedetection.Heuristic{}

	heuristics = append(heuristics, automultilinedetection.NewTokenizer(config.Datadog().GetInt("logs_config.auto_multi_line.tokenizer_max_input_bytes")))

	if config.Datadog().GetBool("logs_config.auto_multi_line.enable_json_detection") {
		heuristics = append(heuristics, automultilinedetection.NewJSONDetector())
	}

	heuristics = append(heuristics, automultilinedetection.NewUserSamples(config.Datadog()))

	if config.Datadog().GetBool("logs_config.auto_multi_line.enable_datetime_detection") {
		heuristics = append(heuristics, automultilinedetection.NewTimestampDetector(
			config.Datadog().GetFloat64("logs_config.auto_multi_line.timestamp_detector_match_threshold")))
	}

	analyticsHeuristics := []automultilinedetection.Heuristic{automultilinedetection.NewPatternTable(
		config.Datadog().GetInt("logs_config.auto_multi_line.pattern_table_max_size"),
		config.Datadog().GetFloat64("logs_config.auto_multi_line.pattern_table_match_threshold"),
		tailerInfo),
	}

	return &AutoMultilineHandler{
		labeler: automultilinedetection.NewLabeler(heuristics, analyticsHeuristics),
		aggregator: automultilinedetection.NewAggregator(
			outputFn,
			maxContentSize,
			flushTimeout,
			config.Datadog().GetBool("logs_config.tag_truncated_logs"),
			config.Datadog().GetBool("logs_config.tag_auto_multi_line_logs"),
			tailerInfo),
	}
}

func (a *AutoMultilineHandler) process(msg *message.Message) {
	label := a.labeler.Label(msg.GetContent())
	a.aggregator.Aggregate(msg, label)
}

func (a *AutoMultilineHandler) flushChan() <-chan time.Time {
	return a.aggregator.FlushChan()
}

func (a *AutoMultilineHandler) flush() {
	a.aggregator.Flush()
}
