//go:build testing

package hub

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/apis"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/henrygd/beszel/internal/geoip"
	"github.com/stretchr/testify/require"
)

const testAgentEgressPath = "/api/beszel/agent-egress"

func agentEgressTestMux(t *testing.T, hub *Hub, systemID string) http.Handler {
	t.Helper()
	t.Setenv("BESZEL_EGRESS_SYSTEM_ID", systemID)
	router, err := apis.NewRouter(hub.App)
	require.NoError(t, err)
	require.NoError(t, hub.registerApiRoutes(&core.ServeEvent{App: hub.App, Router: router}))
	handler, err := router.BuildMux()
	require.NoError(t, err)
	return handler
}

func postAgentEgress(handler http.Handler, token string, headerValues ...string) *httptest.ResponseRecorder {
	// The caller cannot nominate a different IP in the body or ordinary
	// forwarded headers, even when they have the correct probe token.
	req := httptest.NewRequest(http.MethodPost, testAgentEgressPath, strings.NewReader(`{"ip":"9.9.9.9"}`))
	req.Header.Set("X-Token", token)
	req.Header.Set("X-Forwarded-For", "9.9.9.9")
	req.Header.Set("X-Real-IP", "9.9.9.9")
	req.Header.Set("CF-Connecting-IP", "9.9.9.9")
	for _, value := range headerValues {
		req.Header.Add("X-Envoy-External-Address", value)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func createAgentEgressSystem(t *testing.T, hub *Hub, userID, ip string) *core.Record {
	t.Helper()
	record, err := createTestRecord(hub, "systems", map[string]any{
		"name": "Egress fixture", "host": "agent.example.test", "port": "45876", "ip": ip, "users": []string{userID},
	})
	require.NoError(t, err)
	if managed, err := hub.sm.GetSystem(record.Id); err == nil {
		managed.StopUpdater()
	}
	_, err = hub.DB().Update("systems", dbx.Params{"status": "up"}, dbx.HashExp{"id": record.Id}).Execute()
	require.NoError(t, err)
	return record
}

func TestAgentEgressDisabled(t *testing.T) {
	hub, app, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, app)
	handler := agentEgressTestMux(t, hub, "")
	require.Equal(t, http.StatusNotFound, postAgentEgress(handler, "fixture-token", "8.8.8.8").Code)
}

func TestAgentEgressScopeAndIngress(t *testing.T) {
	t.Setenv("BESZEL_HUB_DISABLE_SSH", "true")
	hub, app, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, app)
	owner, err := createTestUser(hub)
	require.NoError(t, err)
	target := createAgentEgressSystem(t, hub, owner.Id, "8.8.8.8")
	second := createAgentEgressSystem(t, hub, owner.Id, "9.9.9.9")
	selectedFingerprint, err := createTestRecord(hub, "fingerprints", map[string]any{
		"system": target.Id, "token": "system-token", "fingerprint": "target-fingerprint",
	})
	require.NoError(t, err)
	otherFingerprint, err := createTestRecord(hub, "fingerprints", map[string]any{
		"system": second.Id, "token": "another-system-token", "fingerprint": "other-fingerprint",
	})
	require.NoError(t, err)
	_, err = createTestRecord(hub, "universal_tokens", map[string]any{
		"user": owner.Id, "token": "permanent-universal-token",
	})
	require.NoError(t, err)
	universalTokenMap.GetMap().Set("active-universal-token", owner.Id, time.Minute)
	t.Cleanup(func() { universalTokenMap.GetMap().Remove("active-universal-token") })

	handler := agentEgressTestMux(t, hub, target.Id)
	for _, test := range []struct {
		name, requestToken, selectedToken string
		headerValues                      []string
		pending, shared                   bool
		wantStatus                        int
	}{
		{name: "missing token", headerValues: []string{"1.1.1.1"}, wantStatus: http.StatusUnauthorized},
		{name: "wrong token", requestToken: "unknown", headerValues: []string{"1.1.1.1"}, wantStatus: http.StatusUnauthorized},
		{name: "other device", requestToken: "another-system-token", headerValues: []string{"1.1.1.1"}, wantStatus: http.StatusUnauthorized},
		{name: "unbound fingerprint", requestToken: "system-token", headerValues: []string{"1.1.1.1"}, pending: true, wantStatus: http.StatusUnauthorized},
		{name: "shared token", requestToken: "system-token", headerValues: []string{"1.1.1.1"}, shared: true, wantStatus: http.StatusUnauthorized},
		{name: "active universal token", requestToken: "active-universal-token", selectedToken: "active-universal-token", headerValues: []string{"1.1.1.1"}, wantStatus: http.StatusUnauthorized},
		{name: "permanent universal token", requestToken: "permanent-universal-token", selectedToken: "permanent-universal-token", headerValues: []string{"1.1.1.1"}, wantStatus: http.StatusUnauthorized},
		{name: "client-controlled headers only", requestToken: "system-token", wantStatus: http.StatusBadRequest},
		{name: "multiple ingress values", requestToken: "system-token", headerValues: []string{"1.1.1.1", "9.9.9.9"}, wantStatus: http.StatusBadRequest},
		{name: "address list", requestToken: "system-token", headerValues: []string{"1.1.1.1, 9.9.9.9"}, wantStatus: http.StatusBadRequest},
		{name: "public IP with port", requestToken: "system-token", headerValues: []string{"1.1.1.1:443"}, wantStatus: http.StatusBadRequest},
		{name: "private ingress address", requestToken: "system-token", headerValues: []string{"10.0.1.2"}, wantStatus: http.StatusBadRequest},
		{name: "CGNAT ingress address", requestToken: "system-token", headerValues: []string{"100.64.0.1"}, wantStatus: http.StatusBadRequest},
		{name: "IPv6 scope", requestToken: "system-token", headerValues: []string{"2001:4860:4860::8888%eth0"}, wantStatus: http.StatusBadRequest},
		{name: "documentation address", requestToken: "system-token", headerValues: []string{"203.0.113.5"}, wantStatus: http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			token := test.selectedToken
			if token == "" {
				token = "system-token"
			}
			fingerprint := "target-fingerprint"
			if test.pending {
				fingerprint = ""
			}
			selectedFingerprint.Set("token", token)
			selectedFingerprint.Set("fingerprint", fingerprint)
			require.NoError(t, hub.Save(selectedFingerprint))
			otherToken := "another-system-token"
			if test.shared {
				otherToken = token
			}
			otherFingerprint.Set("token", otherToken)
			require.NoError(t, hub.Save(otherFingerprint))

			res := postAgentEgress(handler, test.requestToken, test.headerValues...)
			require.Equal(t, test.wantStatus, res.Code, res.Body.String())
			refreshed, err := hub.FindRecordById("systems", target.Id)
			require.NoError(t, err)
			require.Equal(t, "8.8.8.8", refreshed.GetString("ip"))
		})
	}

	selectedFingerprint.Set("token", "system-token")
	require.NoError(t, hub.Save(selectedFingerprint))
	before, err := hub.FindRecordById("systems", target.Id)
	require.NoError(t, err)
	var writes int
	hub.OnRecordUpdate("systems").BindFunc(func(e *core.RecordEvent) error {
		writes++
		return e.Next()
	})
	res := postAgentEgress(handler, "system-token", "8.8.8.8")
	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	require.Zero(t, writes)

	res = postAgentEgress(handler, "system-token", "1.1.1.1")
	require.Equal(t, http.StatusNoContent, res.Code, res.Body.String())
	require.Equal(t, 1, writes)
	updated, err := hub.FindRecordById("systems", target.Id)
	require.NoError(t, err)
	require.Equal(t, "1.1.1.1", updated.GetString("ip"))
	for _, field := range []string{"name", "host", "port", "status", "users"} {
		require.Equal(t, before.GetString(field), updated.GetString(field), field)
	}
	unaffected, err := hub.FindRecordById("systems", second.Id)
	require.NoError(t, err)
	require.Equal(t, "9.9.9.9", unaffected.GetString("ip"))
}

func TestAgentEgressUpdatesRegion(t *testing.T) {
	dbPath := os.Getenv("BESZEL_GEOIP_TEST_DB")
	if dbPath == "" {
		t.Skip("set BESZEL_GEOIP_TEST_DB to a DB-IP City Lite database")
	}
	t.Setenv("BESZEL_GEOIP_DB", dbPath)
	t.Setenv("BESZEL_HUB_DISABLE_SSH", "true")
	hub, app, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, app)
	require.NoError(t, geoip.BindSystems(hub))
	owner, err := createTestUser(hub)
	require.NoError(t, err)
	target := createAgentEgressSystem(t, hub, owner.Id, "8.8.8.8")
	_, err = createTestRecord(hub, "fingerprints", map[string]any{
		"system": target.Id, "token": "system-token", "fingerprint": "target-fingerprint",
	})
	require.NoError(t, err)
	handler := agentEgressTestMux(t, hub, target.Id)
	before, err := hub.FindRecordById("systems", target.Id)
	require.NoError(t, err)
	require.NotEmpty(t, before.GetString("ip_region"))

	probe := postAgentEgress(handler, "system-token", "2001:4860:4860:0::8888")
	require.Equal(t, http.StatusNoContent, probe.Code, probe.Body.String())
	after, err := hub.FindRecordById("systems", target.Id)
	require.NoError(t, err)
	ip := after.GetString("ip")
	require.Equal(t, "2001:4860:4860::8888", ip)
	db, err := geoip.OpenConfigured()
	require.NoError(t, err)
	defer db.Close()
	wantRegion, err := db.Lookup(ip)
	require.NoError(t, err)
	require.NotEmpty(t, wantRegion)
	require.NotEqual(t, before.GetString("ip_region"), wantRegion)
	require.Equal(t, wantRegion, after.GetString("ip_region"))
	require.Equal(t, "up", after.GetString("status"))
}
