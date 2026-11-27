//go:build testing

package hub_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/tools/types"
	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/stretchr/testify/require"
)

func TestNetworkTrafficSummary(t *testing.T) {
	t.Setenv("BESZEL_HUB_SHARE_ALL_SYSTEMS", "false")
	t.Setenv("SHARE_ALL_SYSTEMS", "false")
	hub, handler := firstUserTestMux(t)
	t.Cleanup(hub.Cleanup)
	owner, err := beszelTests.CreateUserWithRole(hub, "traffic-owner@example.com", "password123", "readonly")
	require.NoError(t, err)
	other, err := beszelTests.CreateUserWithRole(hub, "traffic-other@example.com", "password123", "admin")
	require.NoError(t, err)
	ownerToken, err := owner.NewAuthToken()
	require.NoError(t, err)
	otherToken, err := other.NewAuthToken()
	require.NoError(t, err)

	createSystem := func(name, userID string) string {
		t.Helper()
		record, err := beszelTests.CreateRecord(hub, "systems", map[string]any{
			"name": name, "host": name, "users": []string{userID},
		})
		require.NoError(t, err)
		return record.Id
	}
	visible := createSystem("traffic-visible", owner.Id)
	empty := createSystem("traffic-empty", owner.Id)
	hidden := createSystem("traffic-hidden", other.Id)

	now := time.Now().UTC().Truncate(time.Second)
	createSample := func(systemID, period string, created time.Time, stats map[string]any) {
		t.Helper()
		record, err := beszelTests.CreateRecord(hub, "system_stats", map[string]any{
			"system": systemID, "type": period, "stats": stats,
		})
		require.NoError(t, err)
		_, err = hub.DB().NewQuery("UPDATE system_stats SET created = {:created} WHERE id = {:id}").
			Bind(dbx.Params{"created": created.Format(types.DefaultDateLayout), "id": record.Id}).Execute()
		require.NoError(t, err)
	}
	old := now.Add(-3 * time.Hour)
	latest := now.Add(-10 * time.Minute)
	createSample(visible, "480m", old, map[string]any{"b": []int{2, 5}})
	createSample(visible, "120m", old, map[string]any{"b": []int{10, 20}})
	createSample(visible, "1m", latest, map[string]any{"b": []int{1, 1}})
	createSample(visible, "1m", now.Add(-5*time.Minute), map[string]any{"ni": map[string][]int{"eth0": {0, 0, 15, 15}}})
	createSample(visible, "1m", now.Add(-4*time.Minute), map[string]any{"cpu": 1}) // no network reporting
	createSample(visible, "1m", now.Add(-3*time.Minute), map[string]any{"ni": map[string]any{}})
	legacy := now.Add(-15 * time.Hour)
	createSample(hidden, "20m", legacy, map[string]any{"ns": 0.25, "nr": 0.5})
	createSample(visible, "480m", now.Add(-31*24*time.Hour), map[string]any{"b": []int{1000, 1000}})

	get := func(token string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, "/api/beszel/network-traffic", nil)
		request.Header.Set("Authorization", token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	type totals struct {
		ReceivedBytes  uint64     `json:"receivedBytes"`
		SentBytes      uint64     `json:"sentBytes"`
		CoveredSeconds int64      `json:"coveredSeconds"`
		SampleCount    int        `json:"sampleCount"`
		FirstSampleAt  *time.Time `json:"firstSampleAt"`
		LastSampleAt   *time.Time `json:"lastSampleAt"`
	}
	decode := func(response *httptest.ResponseRecorder) map[string]totals {
		t.Helper()
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var data struct {
			From    time.Time         `json:"from"`
			To      time.Time         `json:"to"`
			Systems map[string]totals `json:"systems"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &data))
		require.Equal(t, 30*24*time.Hour, data.To.Sub(data.From))
		return data.Systems
	}

	require.Equal(t, http.StatusUnauthorized, get("").Code)
	owned := decode(get(ownerToken))
	require.Equal(t, map[string]totals{
		visible: {
			ReceivedBytes: 6*3600*5 + 2*3600*20 + 60, SentBytes: 6*3600*2 + 2*3600*10 + 60,
			CoveredSeconds: 8*3600 + 120, SampleCount: 4, FirstSampleAt: &old, LastSampleAt: new(now.Add(-5 * time.Minute)),
		},
		empty: {},
	}, owned)
	require.Equal(t, map[string]totals{
		hidden: {
			ReceivedBytes: (1 << 19) * 1200, SentBytes: (1 << 18) * 1200,
			CoveredSeconds: 1200, SampleCount: 1, FirstSampleAt: &legacy, LastSampleAt: &legacy,
		},
	}, decode(get(otherToken)))

	t.Setenv("BESZEL_HUB_SHARE_ALL_SYSTEMS", "true")
	t.Setenv("SHARE_ALL_SYSTEMS", "true")
	require.NoError(t, hub.SetCollectionAuthSettings())
	shared := decode(get(ownerToken))
	require.Len(t, shared, 3)
	require.Positive(t, shared[hidden].ReceivedBytes)
}
