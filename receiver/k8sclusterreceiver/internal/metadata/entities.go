package metadata

import metadataPkg "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata"

// GetEntityEvents processes metadata updates and returns
// a map of a delta of metadata mapped to each resource.
func GetEntityEvents(old, new map[metadataPkg.ResourceID]*KubernetesMetadata) metadataPkg.EntityEvents {
	out := metadataPkg.NewEntityEvents()

	for id, oldObj := range old {
		if _, ok := new[id]; !ok {
			// An object was present, but no longer is. Create a "delete" event.
			entityChange := out.Events().AppendEmpty()
			entityChange.Id().PutStr(oldObj.ResourceIDKey, string(oldObj.ResourceID))
			entityChange.SetEmptyEntityDelete()
		}
	}

	// All "new" are current objects. Create "state" events.
	for _, newObj := range new {
		entityChange := out.Events().AppendEmpty()
		entityChange.Id().PutStr(newObj.ResourceIDKey, string(newObj.ResourceID))
		state := entityChange.SetEmptyEntityState()
		state.SetEntityType(newObj.EntityType)
		state.Attributes().FromRaw(mapStrToMapAny(newObj.Metadata))
	}

	return out
}

func mapStrToMapAny(m map[string]string) map[string]any {
	r := map[string]any{}
	for k, v := range m {
		r[k] = v
	}
	return r
}
