package geoip

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	_ "ganigeorgiev.com/pocketbase/pocketbase/migrations"
	"github.com/stretchr/testify/require"
)

func TestSystemRegions(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	require.NoError(t, app.Bootstrap())
	t.Cleanup(func() { require.NoError(t, app.ClearBootstrap()) })
	require.NoError(t, app.RunSystemMigrations())

	systems := core.NewBaseCollection("systems")
	systems.Fields.Add(
		&core.TextField{Name: "host"},
		&core.TextField{Name: "ip"},
		&core.TextField{Name: "ip_region", Max: maxRegionLength},
		&core.TextField{Name: "status"},
	)
	require.NoError(t, app.Save(systems))

	regions := map[string]string{
		"81.2.69.142": "英国 · London",
		"81.2.69.143": "英国 · London",
		"8.8.8.8":     "美国",
	}
	var lastStatus string
	app.OnRecordAfterUpdateSuccess("systems").BindFunc(func(e *core.RecordEvent) error {
		lastStatus = e.Record.GetString("status")
		return e.Next()
	})
	var lookups int
	bindSystemRecords(app, func(ip string) (string, error) {
		lookups++
		return regions[ip], nil
	})
	get := func(id string) *core.Record {
		record, err := app.FindRecordById("systems", id)
		require.NoError(t, err)
		return record
	}

	inferred := core.NewRecord(systems)
	inferred.Set("host", "81.2.69.142")
	inferred.Set("ip_region", "ignored")
	inferred.Set("status", "up")
	require.NoError(t, app.Save(inferred))
	first := get(inferred.Id)
	require.Equal(t, "81.2.69.142", first.GetString("ip"))
	require.Equal(t, "英国 · London", first.GetString("ip_region"))

	explicit := core.NewRecord(systems)
	explicit.Set("host", "81.2.69.142")
	explicit.Set("ip", "8.8.8.8")
	require.NoError(t, app.Save(explicit))
	require.Equal(t, "美国", get(explicit.Id).GetString("ip_region"))

	second := get(inferred.Id)
	first.Set("ip", "8.8.8.8")
	first.Set("status", "down")
	first.Set("ip_region", "ignored")
	require.NoError(t, app.Save(first))
	require.Equal(t, "美国", first.GetString("ip_region"))
	require.Equal(t, "美国", get(first.Id).GetString("ip_region"))

	// Simulate an edit made from an older snapshot whose original region
	// already equals the region of the replacement IP.
	second.Set("ip", "81.2.69.143")
	require.NoError(t, app.Save(second))
	record := get(second.Id)
	require.Equal(t, "81.2.69.143", record.GetString("ip"))
	require.Equal(t, "英国 · London", record.GetString("ip_region"))
	require.Equal(t, "down", record.GetString("status"))
	require.Equal(t, "down", lastStatus)

	first = get(inferred.Id)
	first.Set("ip_region", "ignored")
	require.NoError(t, app.Save(first))
	require.Equal(t, "英国 · London", get(first.Id).GetString("ip_region"))

	first = get(inferred.Id)
	before := lookups
	first.Set("status", "up")
	require.NoError(t, app.Save(first))
	require.Equal(t, before, lookups)
	require.Equal(t, "英国 · London", get(first.Id).GetString("ip_region"))

	first = get(inferred.Id)
	first.Set("ip", "10.0.0.1")
	require.NoError(t, app.Save(first))
	require.Empty(t, get(first.Id).GetString("ip_region"))

	first = get(inferred.Id)
	first.Set("ip", "8.8.8.8")
	require.NoError(t, app.Save(first))
	first = get(inferred.Id)
	first.Set("ip", "")
	require.NoError(t, app.Save(first))
	require.Empty(t, get(first.Id).GetString("ip_region"))
	require.Empty(t, get(first.Id).GetString("ip"))
	require.Equal(t, "81.2.69.142", get(first.Id).GetString("host"))
}
