package migrations

import (
	"strings"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

const systemIPMetadataMigration = "1795737600_system_ip_metadata.go"

func TestSystemIPMetadataMigration(t *testing.T) {
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	require.NoError(t, app.Bootstrap())
	t.Cleanup(func() { require.NoError(t, app.ClearBootstrap()) })
	require.NoError(t, app.RunSystemMigrations())

	var priorMigrations core.MigrationsList
	for _, migration := range core.AppMigrations.Items() {
		if migration.File != systemIPMetadataMigration {
			priorMigrations.Add(migration)
		}
	}
	_, err := core.NewMigrationsRunner(app, priorMigrations).Up()
	require.NoError(t, err)

	users, err := app.FindCollectionByNameOrId("users")
	require.NoError(t, err)
	user := core.NewRecord(users)
	user.Set("email", "migration@example.test")
	user.SetPassword("test-password")
	require.NoError(t, app.Save(user))

	systems, err := app.FindCollectionByNameOrId("systems")
	require.NoError(t, err)
	require.Nil(t, systems.Fields.GetByName("ip"))
	require.Nil(t, systems.Fields.GetByName("ip_region"))

	legacy := []struct {
		host, wantIP string
		record       *core.Record
	}{
		{host: "192.0.2.10", wantIP: "192.0.2.10"},
		{host: "2001:db8::42", wantIP: "2001:db8::42"},
		{host: "sandbox.example.test"},
		{host: "/run/beszel/agent.sock"},
	}
	for i := range legacy {
		record := core.NewRecord(systems)
		record.Set("name", legacy[i].host)
		record.Set("host", legacy[i].host)
		record.Set("port", "45876")
		record.Set("status", "up")
		record.Set("users", []string{user.Id})
		require.NoError(t, app.Save(record))
		legacy[i].record = record
	}

	samples, err := app.FindCollectionByNameOrId("system_stats")
	require.NoError(t, err)
	sample := core.NewRecord(samples)
	sample.Set("system", legacy[0].record.Id)
	sample.Set("type", "1m")
	sample.Set("stats", map[string]any{"cpu": 1.5})
	require.NoError(t, app.Save(sample))

	require.NoError(t, app.RunAppMigrations())

	for _, test := range legacy {
		record, err := app.FindRecordById("systems", test.record.Id)
		require.NoError(t, err)
		require.Equal(t, test.wantIP, record.GetString("ip"), test.host)
		require.Empty(t, record.GetString("ip_region"))
		for _, field := range []string{"name", "host", "port", "status", "updated"} {
			require.Equal(t, test.record.GetString(field), record.GetString(field), field)
		}
		require.Equal(t, []string{user.Id}, record.GetStringSlice("users"))
	}

	sampleAfter, err := app.FindRecordById("system_stats", sample.Id)
	require.NoError(t, err)
	require.Equal(t, sample.GetString("system"), sampleAfter.GetString("system"))
	require.Equal(t, sample.GetString("stats"), sampleAfter.GetString("stats"))

	record, err := app.FindRecordById("systems", legacy[2].record.Id)
	require.NoError(t, err)
	record.Set("ip", "203.0.113.55")
	record.Set("ip_region", "中国香港")
	require.NoError(t, app.Save(record))
	record, err = app.FindRecordById("systems", record.Id)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.55", record.GetString("ip"))
	require.Equal(t, "中国香港", record.GetString("ip_region"))
	require.Equal(t, legacy[2].host, record.GetString("host"))

	record.Set("ip", strings.Repeat("1", 65))
	require.Error(t, app.Save(record))
	record.Set("ip", "")
	record.Set("ip_region", strings.Repeat("界", 121))
	require.Error(t, app.Save(record))

	record.Set("ip_region", "")
	require.NoError(t, app.Save(record))
	record, err = app.FindRecordById("systems", record.Id)
	require.NoError(t, err)
	require.Empty(t, record.GetString("ip"))
	require.Empty(t, record.GetString("ip_region"))
}
