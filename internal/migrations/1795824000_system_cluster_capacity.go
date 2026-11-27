package migrations

import (
	"database/sql"
	"encoding/json"
	"errors"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	m "ganigeorgiev.com/pocketbase/pocketbase/migrations"
	"github.com/henrygd/beszel/internal/entities/system"
)

func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}
		collection.Fields.Add(
			&core.JSONField{Id: "system_capacity", Name: "capacity", MaxSize: 512},
			&core.JSONField{Id: "system_capacity_override", Name: "capacity_override", MaxSize: 512},
			&core.BoolField{Id: "system_cluster_excluded", Name: "cluster_excluded"},
		)
		if err := app.Save(collection); err != nil {
			return err
		}
		return backfillSystemCapacity(app)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}
		collection.Fields.RemoveByName("capacity")
		collection.Fields.RemoveByName("capacity_override")
		collection.Fields.RemoveByName("cluster_excluded")
		return app.Save(collection)
	})
}

func backfillSystemCapacity(app core.App) error {
	systems, err := app.FindAllRecords("systems")
	if err != nil {
		return err
	}
	for _, record := range systems {
		var info system.Info
		_ = record.UnmarshalJSONField("info", &info)
		latest, err := app.FindRecordsByFilter("system_stats", "system = {:id}", "-created", 1, 0, dbx.Params{"id": record.Id})
		if err != nil {
			return err
		}
		var stats system.Stats
		if len(latest) > 0 {
			_ = latest[0].UnmarshalJSONField("stats", &stats)
		}
		detailRecord, err := app.FindRecordById("system_details", record.Id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var details *system.Details
		if detailRecord != nil {
			details = &system.Details{
				Threads:     detailRecord.GetInt("threads"),
				MemoryTotal: uint64(max(0, detailRecord.GetInt64("memory"))),
			}
		}
		capacity := system.ReportedCapacity(system.Capacity{}, record.GetString("host"), record.GetString("ip"), info, stats, details)
		if capacity == (system.Capacity{}) {
			continue
		}
		encoded, err := json.Marshal(capacity)
		if err != nil {
			return err
		}
		// Keep prior statistics, status and record modification time intact.
		if _, err := app.DB().Update("systems", dbx.Params{"capacity": string(encoded)}, dbx.HashExp{"id": record.Id}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
