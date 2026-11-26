package migrations

import (
	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
	"github.com/henrygd/beszel/internal/geoip"
)

func init() {
	m.Register(func(app core.App) error {
		database, err := geoip.OpenConfigured()
		if err != nil || database == nil {
			return err
		}
		defer func() { _ = database.Close() }()

		systems, err := app.FindAllRecords("systems", dbx.NewExp("[[ip]] != '' OR [[ip_region]] != ''"))
		if err != nil {
			return err
		}
		for _, system := range systems {
			region, err := database.Lookup(system.GetString("ip"))
			if err != nil {
				return err
			}
			if region == system.GetString("ip_region") {
				continue
			}
			// Preserve record modification times and bypass monitoring hooks.
			_, err = app.DB().Update("systems",
				dbx.Params{"ip_region": region},
				dbx.HashExp{"id": system.Id},
			).Execute()
			if err != nil {
				return err
			}
		}
		return nil
	}, nil)
}
