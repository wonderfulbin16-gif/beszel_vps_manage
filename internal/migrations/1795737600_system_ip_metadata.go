package migrations

import (
	"net/netip"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}
		collection.Fields.Add(
			&core.TextField{Id: "system_ip", Name: "ip", Max: 64},
			&core.TextField{Id: "system_ip_region", Name: "ip_region", Max: 120},
		)
		if err := app.Save(collection); err != nil {
			return err
		}

		systems, err := app.FindAllRecords(collection, dbx.HashExp{"ip": ""})
		if err != nil {
			return err
		}
		for _, system := range systems {
			address, err := netip.ParseAddr(strings.TrimSpace(system.GetString("host")))
			if err != nil {
				continue
			}
			// Backfill this field without running record hooks or changing modification times.
			_, err = app.DB().Update(collection.Name,
				dbx.Params{"ip": address.WithZone("").String()},
				dbx.HashExp{"id": system.Id},
			).Execute()
			if err != nil {
				return err
			}
		}
		return nil
	}, nil)
}
