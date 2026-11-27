//go:build testing

package hub

import (
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"
	"github.com/stretchr/testify/require"
)

func TestNetworkTrafficClipsWindowAndPrefersFineSamples(t *testing.T) {
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(2 * time.Hour)
	newSample := func(period string, end time.Time, rate float64) networkTrafficSample {
		created, err := types.ParseDateTime(end)
		require.NoError(t, err)
		return networkTrafficSample{Period: period, Created: created, Sent: rate, Received: rate * 2}
	}

	// The coarse sample spans the beginning of the requested range. Two finer
	// intervals cover its first and last 10 minutes inside that range.
	coarseEnd := from.Add(30 * time.Minute)
	firstFineEnd := from.Add(10 * time.Minute)
	tail := from.Add(time.Hour)
	samples := []networkTrafficSample{
		newSample("480m", coarseEnd, 1),
		newSample("10m", firstFineEnd, 5),
		newSample("10m", coarseEnd, 10),
		newSample("20m", coarseEnd, 20), // overlaps the latter fine sample and part of the coarse sample
		newSample("1m", tail, 0),
	}
	totals := sumNetworkTraffic(samples, from, to)
	require.Equal(t, uint64(600*(5+10+20)), totals.SentBytes)
	require.Equal(t, totals.SentBytes*2, totals.ReceivedBytes)
	require.Equal(t, int64(31*60), totals.CoveredSeconds)
	require.Equal(t, 4, totals.SampleCount)
	require.Equal(t, &firstFineEnd, totals.FirstSampleAt)
	require.Equal(t, &tail, totals.LastSampleAt)
}
