// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-present Datadog, Inc.

package flare

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/DataDog/datadog-agent/comp/aggregator/demultiplexer/demultiplexerimpl"
	"github.com/DataDog/datadog-agent/comp/collector/collector"
	"github.com/DataDog/datadog-agent/comp/core/autodiscovery"
	"github.com/DataDog/datadog-agent/comp/core/autodiscovery/autodiscoveryimpl"
	"github.com/DataDog/datadog-agent/comp/core/autodiscovery/scheduler"
	"github.com/DataDog/datadog-agent/comp/core/config"
	flarebuilder "github.com/DataDog/datadog-agent/comp/core/flare/builder"
	"github.com/DataDog/datadog-agent/comp/core/flare/helpers"
	"github.com/DataDog/datadog-agent/comp/core/flare/types"
	"github.com/DataDog/datadog-agent/comp/core/hostname/hostnameimpl"
	log "github.com/DataDog/datadog-agent/comp/core/log/def"
	logmock "github.com/DataDog/datadog-agent/comp/core/log/mock"
	"github.com/DataDog/datadog-agent/comp/core/secrets/secretsimpl"
	tagger "github.com/DataDog/datadog-agent/comp/core/tagger/def"
	mockTagger "github.com/DataDog/datadog-agent/comp/core/tagger/mock"
	nooptelemetry "github.com/DataDog/datadog-agent/comp/core/telemetry/noopsimpl"
	workloadmeta "github.com/DataDog/datadog-agent/comp/core/workloadmeta/def"
	workloadmetafxmock "github.com/DataDog/datadog-agent/comp/core/workloadmeta/fx-mock"
	"github.com/DataDog/datadog-agent/pkg/util/fxutil"
)

func TestFlareCreation(t *testing.T) {
	realProvider := types.NewFiller(func(_ types.FlareBuilder) error { return nil })

	fakeTagger := mockTagger.SetupFakeTagger(t)

	f := newFlare(
		fxutil.Test[dependencies](
			t,
			fx.Provide(func() log.Component { return logmock.New(t) }),
			config.MockModule(),
			secretsimpl.MockModule(),
			nooptelemetry.Module(),
			hostnameimpl.MockModule(),
			demultiplexerimpl.MockModule(),
			fx.Provide(func() Params { return Params{} }),
			collector.NoneModule(),
			workloadmetafxmock.MockModule(workloadmeta.NewParams()),
			autodiscoveryimpl.MockModule(),
			fx.Supply(autodiscoveryimpl.MockParams{Scheduler: scheduler.NewController()}),
			fx.Provide(func(ac autodiscovery.Mock) autodiscovery.Component { return ac.(autodiscovery.Component) }),
			fx.Provide(func() mockTagger.Mock { return fakeTagger }),
			fx.Provide(func() tagger.Component { return fakeTagger }),
			// provider a nil FlareFiller
			fx.Provide(fx.Annotate(
				func() *types.FlareFiller { return nil },
				fx.ResultTags(`group:"flare"`),
			)),
			// provider a real FlareFiller
			fx.Provide(fx.Annotate(
				func() *types.FlareFiller { return realProvider },
				fx.ResultTags(`group:"flare"`),
			)),
		),
	)

	assert.GreaterOrEqual(t, len(f.Comp.(*flare).providers), 1)
	assert.NotContains(t, f.Comp.(*flare).providers, nil)
}

func TestRunProviders(t *testing.T) {
	firstStarted := make(chan struct{}, 1)
	var secondDone atomic.Bool

	fakeTagger := mockTagger.SetupFakeTagger(t)

	deps := fxutil.Test[dependencies](
		t,
		fx.Provide(func() log.Component { return logmock.New(t) }),
		config.MockModule(),
		secretsimpl.MockModule(),
		nooptelemetry.Module(),
		hostnameimpl.MockModule(),
		demultiplexerimpl.MockModule(),
		fx.Provide(func() Params { return Params{} }),
		collector.NoneModule(),
		workloadmetafxmock.MockModule(workloadmeta.NewParams()),
		autodiscoveryimpl.MockModule(),
		fx.Supply(autodiscoveryimpl.MockParams{Scheduler: scheduler.NewController()}),
		fx.Provide(func(ac autodiscovery.Mock) autodiscovery.Component { return ac.(autodiscovery.Component) }),
		fx.Provide(func() mockTagger.Mock { return fakeTagger }),
		fx.Provide(func() tagger.Component { return fakeTagger }),
		// provider a nil FlareFiller
		fx.Provide(fx.Annotate(
			func() *types.FlareFiller { return nil },
			fx.ResultTags(`group:"flare"`),
		)),
		fx.Provide(fx.Annotate(
			func() *types.FlareFiller {
				return types.NewFiller(func(_ types.FlareBuilder) error {
					firstStarted <- struct{}{}
					return nil
				})
			},
			fx.ResultTags(`group:"flare"`),
		)),
		fx.Provide(fx.Annotate(
			func() *types.FlareFiller {
				return types.NewFiller(func(_ types.FlareBuilder) error {
					time.Sleep(10 * time.Second)
					secondDone.Store(true)
					return nil
				})
			},
			fx.ResultTags(`group:"flare"`),
		)),
	)

	cliProviderTimeout := time.Nanosecond
	f := newFlare(deps)

	fb, err := helpers.NewFlareBuilder(false, flarebuilder.FlareArgs{})
	require.NoError(t, err)

	start := time.Now()
	f.Comp.(*flare).runProviders(fb, cliProviderTimeout)
	// ensure that providers are actually started
	<-firstStarted
	elapsed := time.Since(start)

	// ensure that we're not blocking for the slow provider
	assert.Less(t, elapsed, 5*time.Second)
	assert.False(t, secondDone.Load())
}
