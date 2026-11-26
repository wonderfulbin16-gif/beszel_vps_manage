package migrations

import (
	"os"
	"testing"

	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/require"
)

const systemIPRegionsMigration = "1795737601_system_ip_regions.go"

func TestSystemIPRegionsMigration(t *testing.T) {
	dbPath := os.Getenv("BESZEL_GEOIP_TEST_DB")
	if dbPath == "" {
		t.Skip("set BESZEL_GEOIP_TEST_DB to a DB-IP City Lite database")
	}
	t.Setenv("BESZEL_GEOIP_DB", dbPath)

	app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir()})
	require.NoError(t, app.Bootstrap())
	t.Cleanup(func() { require.NoError(t, app.ClearBootstrap()) })
	require.NoError(t, app.RunSystemMigrations())

	var priorMigrations core.MigrationsList
	for _, migration := range core.AppMigrations.Items() {
		if migration.File < systemIPRegionsMigration {
			priorMigrations.Add(migration)
		}
	}
	_, err := core.NewMigrationsRunner(app, priorMigrations).Up()
	require.NoError(t, err)

	systems, err := app.FindCollectionByNameOrId("systems")
	require.NoError(t, err)
	testSystems := []struct {
		ip, region string
		hasRegion  bool
	}{
		{ip: "81.2.69.142", hasRegion: true},
		{ip: "8.8.8.8", region: "stale", hasRegion: true},
		{ip: "10.0.0.5", region: "stale"},
		{region: "stale"},
	}

	users, err := app.FindCollectionByNameOrId("users")
	require.NoError(t, err)
	user := core.NewRecord(users)
	user.Set("email", "migration@example.test")
	user.SetPassword("test-password")
	require.NoError(t, app.Save(user))

	for _, test := range testSystems {
		record := core.NewRecord(systems)
		record.Set("name", "seed")
		record.Set("host", "connection.example.test")
		record.Set("port", "45876")
		record.Set("status", "up")
		record.Set("users", []string{user.Id})
		record.Set("ip", test.ip)
		record.Set("ip_region", test.region)
		require.NoError(t, app.Save(record))
	}

	before, err := app.FindAllRecords(systems)
	require.NoError(t, err)
	require.NoError(t, app.RunAppMigrations())

	for _, existing := range before {
		record, err := app.FindRecordById("systems", existing.Id)
		require.NoError(t, err)
		for _, test := range testSystems {
			if record.GetString("ip") != test.ip {
				continue
			}
			region := record.GetString("ip_region")
			if test.hasRegion {
				require.NotEmpty(t, region, test.ip)
				require.NotEqual(t, test.region, region, test.ip)
			} else {
				require.Empty(t, region, test.ip)
			}
		}
		for _, field := range []string{"name", "host", "port", "status", "users", "ip", "updated"} {
			require.Equal(t, existing.GetString(field), record.GetString(field), field)
		}
	}
}
