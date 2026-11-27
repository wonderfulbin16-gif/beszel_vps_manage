package migrations

import (
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/stretchr/testify/require"
)

func TestSystemClusterCapacityBackfill(t *testing.T) {
	t.Setenv("BESZEL_GEOIP_DB", "")
	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	require.NoError(t, app.Bootstrap())
	t.Cleanup(func() { require.NoError(t, app.ClearBootstrap()) })
	require.NoError(t, app.RunSystemMigrations())
	var before core.MigrationsList
	for _, migration := range core.AppMigrations.Items() {
		if migration.File < "1795824000_system_cluster_capacity.go" {
			before.Add(migration)
		}
	}
	_, err := core.NewMigrationsRunner(app, before).Up()
	require.NoError(t, err)

	users, err := app.FindCollectionByNameOrId("users")
	require.NoError(t, err)
	user := core.NewRecord(users)
	user.Set("email", "cluster@example.test")
	user.SetPassword("test-password")
	require.NoError(t, app.Save(user))
	systems, err := app.FindCollectionByNameOrId("systems")
	require.NoError(t, err)
	details, err := app.FindCollectionByNameOrId("system_details")
	require.NoError(t, err)
	stats, err := app.FindCollectionByNameOrId("system_stats")
	require.NoError(t, err)

	var records []*core.Record
	for _, host := range []string{"vm.example.test", "/dev/null"} {
		record := core.NewRecord(systems)
		record.Set("name", host)
		record.Set("host", host)
		record.Set("status", "down")
		record.Set("users", []string{user.Id})
		record.Set("info", system.Info{Threads: 96, SandboxDiskEnabled: host == "/dev/null"})
		require.NoError(t, app.Save(record))
		records = append(records, record)

		detail := core.NewRecord(details)
		detail.Id = record.Id
		detail.Set("system", record.Id)
		detail.Set("threads", 4)
		detail.Set("memory", 8<<30)
		require.NoError(t, app.SaveNoValidate(detail))

		sample := core.NewRecord(stats)
		sample.Set("system", record.Id)
		sample.Set("type", "120m")
		sample.Set("stats", system.Stats{MemBytes: [4]uint64{4 << 30}, DiskTotal: 40})
		require.NoError(t, app.Save(sample))
	}

	require.NoError(t, app.RunAppMigrations())
	for i, want := range []system.Capacity{{CPU: 4, Memory: 4, Disk: 40}, {Memory: 4}} {
		updated, err := app.FindRecordById("systems", records[i].Id)
		require.NoError(t, err)
		var got system.Capacity
		require.NoError(t, updated.UnmarshalJSONField("capacity", &got))
		require.Equal(t, want, got)
		require.False(t, updated.GetBool("cluster_excluded"))
		require.Equal(t, "down", updated.GetString("status"))
		require.Equal(t, records[i].GetString("updated"), updated.GetString("updated"))
	}
}
