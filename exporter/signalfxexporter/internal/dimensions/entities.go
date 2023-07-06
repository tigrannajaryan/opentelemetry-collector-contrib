package dimensions

import (
	"encoding/json"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.uber.org/multierr"

	metadata "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata"
)

func (dc *DimensionClient) PushEntityEvents(events metadata.EntityEvents) error {
	var errs error
	for i := 0; i < events.Events().Len(); i++ {
		event := events.Events().At(i)
		entityId := entityId2comparable(event.Id())
		var dimUpdate *DimensionUpdate
		switch event.EventType() {
		case metadata.EventTypeState:
			oldState, exists := dc.observedEntities[entityId]
			if exists {
				dimUpdate = calcEntityStateDelta(event.Id(), oldState, event.EntityState())
			} else {
				dc.observedEntities[entityId] = event.EntityState()
				dimUpdate = calcEntityStateNew(event.Id(), event.EntityState())
			}
		case metadata.EventTypeDelete:
			delete(dc.observedEntities, entityId)
		default:
			// Unknown event type. Skip.
			continue
		}
		errs = multierr.Append(errs, dc.acceptDimension(dimUpdate))
	}
	return errs
}

func calcEntityStateNew(entityId pcommon.Map, state metadata.EntityState) *DimensionUpdate {
	var du DimensionUpdate

	entityIdToDimension(entityId, &du)

	state.Attributes().Range(
		func(k string, v pcommon.Value) bool {
			s := v.AsString()
			du.Properties[sanitizeProperty(k)] = &s
			return true
		},
	)
	return &du
}

func entityIdToDimension(entityId pcommon.Map, du *DimensionUpdate) {
	entityId.Range(
		func(k string, v pcommon.Value) bool {
			// Note: Signalfx does not support multiple dimensions per id. If our source has
			// multiple dimensions there is not much we can do, so we do the simplest thing
			// and concatenate the dimensions.
			// The sources we care about use only one dimensions for now (the k8scluster receiver).
			// In the future if other sources appear that we need to support then the signalfx
			// backend needs to change.
			du.Name += k
			du.Value += v.AsString()
			return true
		},
	)
}

func calcEntityStateDelta(
	entityId pcommon.Map,
	oldState metadata.EntityState,
	newState metadata.EntityState,
) *DimensionUpdate {
	var du DimensionUpdate

	entityIdToDimension(entityId, &du)

	// Add all attributes in new state to du.Properties map. Some of these attributes
	// may be new, some may be existing. We will remove then ones that were existing
	// in the next step.
	newState.Attributes().Range(
		func(k string, v pcommon.Value) bool {
			s := v.AsString()
			du.Properties[sanitizeProperty(k)] = &s
			return true
		},
	)

	// Now iterate over attrivutes that we had in old state.
	oldState.Attributes().Range(
		func(k string, v pcommon.Value) bool {
			key := sanitizeProperty(k)
			if _, existed := du.Properties[key]; existed {
				// This attributed existed in the old state and exists in the new state.
				// No need to send it again.
				delete(du.Properties, key)
			} else {
				// This attribute existed in the old state, but does nt exist in the new state.
				// Need to remove it. To request removal we set the value of the key to nil.
				du.Properties[key] = nil
			}
			return true
		},
	)
	return &du

}

func entityId2comparable(id pcommon.Map) entityId {
	jsonStr, _ := json.Marshal(id.AsRaw())
	return entityId(jsonStr)
}
