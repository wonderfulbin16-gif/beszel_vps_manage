//go:build testing

package systems

import (
	"context"
	"testing"

	"ganigeorgiev.com/pocketbase/dbx"
	"ganigeorgiev.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingRecordHub fails record lookups with err, like a query that times out
// while the hub's VM is frozen.
type failingRecordHub struct {
	stubHub
	err error
}

func (h failingRecordHub) FindRecordById(collectionModelOrIdentifier any, recordId string, optFilters ...func(q *dbx.SelectQuery) error) (*core.Record, error) {
	return nil, h.err
}

func TestGetRecordKeepsSystemOnTransientError(t *testing.T) {
	sys, app := newTestSystemWithHub(t)
	sm := NewSystemManager(failingRecordHub{stubHub: stubHub{app}, err: context.DeadlineExceeded})
	t.Cleanup(sm.cancel)
	sys.manager = sm
	sys.ctx, sys.cancel = sys.getContext(sm.ctx)
	sm.systems.Set(sys.Id, sys)

	record, err := sys.getRecord(sm.hub)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, record)
	assert.True(t, sm.HasSystem(sys.Id), "system should stay in the manager")
	assert.NoError(t, sys.ctx.Err(), "updater context should not be cancelled")
}

func TestGetRecordRemovesDeletedSystem(t *testing.T) {
	sys, app := newTestSystemWithHub(t)
	sm := NewSystemManager(stubHub{app})
	t.Cleanup(sm.cancel)
	sys.manager = sm
	sys.ctx, sys.cancel = sys.getContext(sm.ctx)
	sm.systems.Set(sys.Id, sys)

	record, err := app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	require.NoError(t, app.Delete(record))

	_, err = sys.getRecord(sm.hub)
	require.Error(t, err)
	assert.False(t, sm.HasSystem(sys.Id), "deleted system should be removed")
	assert.Error(t, sys.ctx.Err(), "updater context should be cancelled")
}

func TestBackgroundRecordUpdatePreservesConcurrentIPMetadata(t *testing.T) {
	sys, app := newTestSystemWithHub(t)
	background, err := sys.getRecord(app)
	require.NoError(t, err)

	userEdit, err := app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	userEdit.Set("ip", "2001:db8::5")
	userEdit.Set("ip_region", "Tokyo")
	require.NoError(t, app.SaveNoValidate(userEdit))

	background.Set("status", down)
	require.NoError(t, app.SaveNoValidate(background))

	stored, err := app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	assert.Equal(t, "2001:db8::5", stored.GetString("ip"))
	assert.Equal(t, "Tokyo", stored.GetString("ip_region"))
	assert.Equal(t, down, stored.GetString("status"))
}

func TestSystemCapacityRequestRejectsAutoEditsAndValidatesOverride(t *testing.T) {
	sys, app := newTestSystemWithHub(t)
	record, err := app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	request := &core.RecordRequestEvent{Record: record, RequestEvent: &core.RequestEvent{App: app}}
	freshRequest := &core.RecordRequestEvent{Record: core.NewRecord(record.Collection()), RequestEvent: request.RequestEvent}
	require.NoError(t, validateSystemCapacityRequest(freshRequest))
	freshRequest.Record.Set("capacity", map[string]float64{"cpu": 128})
	require.ErrorContains(t, validateSystemCapacityRequest(freshRequest), "read-only")

	record.Set("capacity", map[string]float64{"cpu": 128})
	require.ErrorContains(t, validateSystemCapacityRequest(request), "read-only")
	record.Set("capacity", record.Original().Get("capacity"))

	record.Set("capacity_override", map[string]float64{"disk": -1})
	require.ErrorContains(t, validateSystemCapacityRequest(request), "Invalid capacity correction")
	record.Set("capacity_override", map[string]float64{"disk": 40})
	record.Set("cluster_excluded", true)
	require.NoError(t, validateSystemCapacityRequest(request))
	require.NoError(t, app.SaveNoValidate(record))

	stored, err := app.FindRecordById("systems", sys.Id)
	require.NoError(t, err)
	var override map[string]float64
	require.NoError(t, stored.UnmarshalJSONField("capacity_override", &override))
	require.Equal(t, map[string]float64{"disk": 40}, override)
	require.True(t, stored.GetBool("cluster_excluded"))
}
