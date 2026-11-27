package hub

import (
	"cmp"
	"math"
	"net/http"
	"slices"
	"sort"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/search"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"
)

const networkTrafficLookback = 30 * 24 * time.Hour

var trafficSamplePeriods = map[string]int64{
	"1m":   time.Minute.Milliseconds(),
	"10m":  (10 * time.Minute).Milliseconds(),
	"20m":  (20 * time.Minute).Milliseconds(),
	"120m": (120 * time.Minute).Milliseconds(),
	"480m": (480 * time.Minute).Milliseconds(),
}

type networkTrafficTotals struct {
	ReceivedBytes  uint64     `json:"receivedBytes"`
	SentBytes      uint64     `json:"sentBytes"`
	CoveredSeconds int64      `json:"coveredSeconds"`
	SampleCount    int        `json:"sampleCount"`
	FirstSampleAt  *time.Time `json:"firstSampleAt,omitempty"`
	LastSampleAt   *time.Time `json:"lastSampleAt,omitempty"`
}

type networkTrafficSample struct {
	System   string         `db:"system"`
	Period   string         `db:"type"`
	Created  types.DateTime `db:"created"`
	Sent     float64        `db:"sent"`
	Received float64        `db:"received"`
}

func getNetworkTraffic(e *core.RequestEvent) error {
	requestInfo, err := e.RequestInfo()
	if err != nil {
		return err
	}
	systemsQuery, err := trafficListQuery(e, requestInfo, "systems")
	if err != nil {
		return err
	}
	var systems []struct {
		ID string `db:"id"`
	}
	if err := systemsQuery.Select("systems.id").All(&systems); err != nil {
		return err
	}

	to := time.Now().UTC().Truncate(time.Millisecond)
	from := to.Add(-networkTrafficLookback)
	result := struct {
		From    time.Time                       `json:"from"`
		To      time.Time                       `json:"to"`
		Systems map[string]networkTrafficTotals `json:"systems"`
	}{From: from, To: to, Systems: make(map[string]networkTrafficTotals, len(systems))}
	if len(systems) > 0 {
		ids := make([]any, 0, len(systems))
		for _, system := range systems {
			ids = append(ids, system.ID)
			result.Systems[system.ID] = networkTrafficTotals{}
		}

		query, err := trafficListQuery(e, requestInfo, "system_stats")
		if err != nil {
			return err
		}
		periods := make([]any, 0, len(trafficSamplePeriods))
		for period := range trafficSamplePeriods {
			periods = append(periods, period)
		}
		var samples []networkTrafficSample
		err = query.Select(
			"system_stats.system", "system_stats.type", "system_stats.created",
			"coalesce(json_extract(system_stats.stats, '$.b[0]'), json_extract(system_stats.stats, '$.ns') * 1048576, 0) AS sent",
			"coalesce(json_extract(system_stats.stats, '$.b[1]'), json_extract(system_stats.stats, '$.nr') * 1048576, 0) AS received",
		).AndWhere(dbx.In("system_stats.system", ids...)).AndWhere(dbx.In("system_stats.type", periods...)).
			AndWhere(dbx.NewExp("system_stats.created > {:trafficFrom} AND system_stats.created <= {:trafficTo}", dbx.Params{
				"trafficFrom": from.Format(types.DefaultDateLayout), "trafficTo": to.Format(types.DefaultDateLayout),
			})).
			// All-zero modern rates omit b. A tracked interface still establishes coverage.
			AndWhere(dbx.NewExp(`json_type(system_stats.stats, '$.b[0]') IN ('integer', 'real')
				OR json_type(system_stats.stats, '$.b[1]') IN ('integer', 'real')
				OR json_type(system_stats.stats, '$.ns') IN ('integer', 'real')
				OR json_type(system_stats.stats, '$.nr') IN ('integer', 'real')
				OR (json_type(system_stats.stats, '$.ni') = 'object' AND EXISTS (SELECT 1 FROM json_each(system_stats.stats, '$.ni')))`)).
			All(&samples)
		if err != nil {
			return err
		}

		bySystem := make(map[string][]networkTrafficSample, len(systems))
		for _, sample := range samples {
			bySystem[sample.System] = append(bySystem[sample.System], sample)
		}
		for systemID, history := range bySystem {
			result.Systems[systemID] = sumNetworkTraffic(history, from, to)
		}
	}

	e.Response.Header().Set("Cache-Control", "private, no-store")
	return e.JSON(http.StatusOK, result)
}

// trafficListQuery applies the same collection access rules as the normal records API.
func trafficListQuery(e *core.RequestEvent, info *core.RequestInfo, collectionName string) (*dbx.SelectQuery, error) {
	collection, err := e.App.FindCachedCollectionByNameOrId(collectionName)
	if err != nil {
		return nil, err
	}
	query := e.App.RecordQuery(collection).WithContext(e.Request.Context())
	if info.HasSuperuserAuth() {
		return query, nil
	}
	if collection.ListRule == nil {
		return nil, e.ForbiddenError("Only superusers can perform this action.", nil)
	}
	if *collection.ListRule != "" {
		resolver := core.NewRecordFieldResolver(e.App, collection, info, true)
		expression, err := search.FilterData(*collection.ListRule).BuildExpr(resolver)
		if err != nil {
			return nil, err
		}
		query.AndWhere(expression)
		if err := resolver.UpdateQuery(query); err != nil {
			return nil, err
		}
	}
	return query, nil
}

func sumNetworkTraffic(samples []networkTrafficSample, from, to time.Time) networkTrafficTotals {
	// Use the more granular measurement first whenever different storage tiers overlap.
	slices.SortFunc(samples, func(a, b networkTrafficSample) int {
		if order := cmp.Compare(trafficSamplePeriods[a.Period], trafficSamplePeriods[b.Period]); order != 0 {
			return order
		}
		return a.Created.Compare(b.Created)
	})
	var totals networkTrafficTotals
	var covered []trafficInterval
	var sent, received float64
	var coveredMillis int64
	fromMillis, toMillis := from.UnixMilli(), to.UnixMilli()
	for _, sample := range samples {
		if sample.Created.IsZero() || sample.Sent < 0 || sample.Received < 0 ||
			math.IsNaN(sample.Sent) || math.IsNaN(sample.Received) || math.IsInf(sample.Sent, 0) || math.IsInf(sample.Received, 0) {
			continue
		}
		end := sample.Created.Time().UnixMilli()
		start, stop := max(end-trafficSamplePeriods[sample.Period], fromMillis), min(end, toMillis)
		if start >= stop {
			continue
		}
		var addedMillis int64
		covered, addedMillis = addTrafficInterval(covered, start, stop)
		if addedMillis == 0 {
			continue
		}
		coveredMillis += addedMillis
		seconds := float64(addedMillis) / 1000
		sent += sample.Sent * seconds
		received += sample.Received * seconds
		totals.SampleCount++
		created := sample.Created.Time().UTC()
		if totals.FirstSampleAt == nil || created.Before(*totals.FirstSampleAt) {
			totals.FirstSampleAt = &created
		}
		if totals.LastSampleAt == nil || created.After(*totals.LastSampleAt) {
			totals.LastSampleAt = &created
		}
	}
	totals.CoveredSeconds = (coveredMillis + 500) / 1000
	totals.SentBytes = uint64(math.Round(sent))
	totals.ReceivedBytes = uint64(math.Round(received))
	return totals
}

type trafficInterval struct{ start, stop int64 }

// addTrafficInterval adds a closed-open range to a sorted, disjoint union and
// returns how much time the sample contributes after overlap is removed.
func addTrafficInterval(covered []trafficInterval, start, stop int64) ([]trafficInterval, int64) {
	first := sort.Search(len(covered), func(i int) bool { return covered[i].stop >= start })
	last := first
	remaining := stop - start
	merged := trafficInterval{start, stop}
	for last < len(covered) && covered[last].start <= stop {
		rangePart := covered[last]
		remaining -= min(stop, rangePart.stop) - max(start, rangePart.start)
		merged.start = min(merged.start, rangePart.start)
		merged.stop = max(merged.stop, rangePart.stop)
		last++
	}
	if remaining == 0 {
		return covered, 0
	}
	return slices.Replace(covered, first, last, merged), remaining
}
