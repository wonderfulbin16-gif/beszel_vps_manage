package geoip

import (
	"errors"
	"net/netip"
	"strings"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
)

// BindSystems derives the region for new systems and whenever their metadata IP changes.
func BindSystems(app core.App) error {
	database, err := OpenConfigured()
	if err != nil || database == nil {
		return err
	}
	app.OnTerminate().BindFunc(func(e *core.TerminateEvent) error {
		return errors.Join(e.Next(), database.Close())
	})
	bindSystemRecords(app, database.Lookup)
	return nil
}

func bindSystemRecords(app core.App, lookup func(string) (string, error)) {
	app.OnRecordCreate("systems").BindFunc(func(e *core.RecordEvent) error {
		ip := e.Record.GetString("ip")
		if strings.TrimSpace(ip) == "" {
			if address, err := netip.ParseAddr(strings.TrimSpace(e.Record.GetString("host"))); err == nil {
				ip = address.WithZone("").String()
				e.Record.Set("ip", ip)
			}
		}
		region, err := lookup(ip)
		if err != nil {
			return err
		}
		e.Record.Set("ip_region", region)
		return e.Next()
	})

	app.OnRecordUpdate("systems").BindFunc(func(e *core.RecordEvent) error {
		previous := e.Record.Original()
		ip := e.Record.GetString("ip")
		if ip == previous.GetString("ip") && e.Record.GetString("ip_region") == previous.GetString("ip_region") {
			return e.Next()
		}
		region, err := lookup(ip)
		if err != nil {
			return err
		}

		e.Record.IgnoreUnchangedFields(true)

		// Write both metadata fields in one transaction even when the
		// computed region matches the original. Another edit may have
		// changed the persisted IP and region since this record was fetched.
		return e.App.RunInTransaction(func(txApp core.App) error {
			app := e.App
			e.App = txApp
			defer func() { e.App = app }()

			// A monitor may have updated its status since this edit was loaded.
			// Let the downstream system hooks see that status.
			if e.Record.GetString("status") == previous.GetString("status") {
				current, err := txApp.FindRecordById("systems", e.Record.Id)
				if err != nil {
					return err
				}
				e.Record.Set("status", current.GetString("status"))
			}
			e.Record.Set("ip_region", previous.GetString("ip_region"))
			if err := e.Next(); err != nil {
				return err
			}
			_, err := txApp.DB().Update("systems",
				dbx.Params{"ip_region": region},
				dbx.HashExp{"id": e.Record.Id, "ip": ip},
			).WithContext(e.Context).Execute()
			if err != nil {
				return err
			}
			e.Record.Set("ip_region", region)
			return nil
		})
	})
}
