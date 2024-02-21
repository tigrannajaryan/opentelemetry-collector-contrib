// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package metadata // import "github.com/open-telemetry/opentelemetry-collector-contrib/receiver/k8sclusterreceiver/internal/metadata"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pentity"

	metadataPkg "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata"
)

// GetEntityEvents processes metadata updates and returns entity events that describe the metadata changes.
func GetEntityEvents(oldMetadata, newMetadata map[metadataPkg.ResourceID]*KubernetesMetadata, timestamp pcommon.Timestamp) metadataPkg.EntityEventsSlice {
	out := metadataPkg.NewEntityEventsSlice()
	out2 := pentity.NewEntityEventSlice()

	for id, oldObj := range oldMetadata {
		if _, ok := newMetadata[id]; !ok {
			// An object was present, but no longer is. Create a "delete" event.
			entityEvent := out.AppendEmpty()
			entityEvent.SetTimestamp(timestamp)
			entityEvent.ID().PutStr(oldObj.ResourceIDKey, string(oldObj.ResourceID))
			entityEvent.SetEntityDelete()

			entityEvent2 := out2.AppendEmpty()
			entityEvent2.SetTimestamp(timestamp)
			entityEvent2.Id().PutStr(oldObj.ResourceIDKey, string(oldObj.ResourceID))
			entityEvent2.SetEmptyEntityDelete()
		}
	}

	// All "new" are current objects. Create "state" events. "old" state does not matter.
	for _, newObj := range newMetadata {
		entityEvent := out.AppendEmpty()
		entityEvent.SetTimestamp(timestamp)
		entityEvent.ID().PutStr(newObj.ResourceIDKey, string(newObj.ResourceID))
		state := entityEvent.SetEntityState()
		state.SetEntityType(newObj.EntityType)

		attrs := state.Attributes()
		for k, v := range newObj.Metadata {
			attrs.PutStr(k, v)
		}

		entityEvent2 := out2.AppendEmpty()
		entityEvent2.SetTimestamp(timestamp)
		entityEvent2.Id().PutStr(newObj.ResourceIDKey, string(newObj.ResourceID))
		state2 := entityEvent2.SetEmptyEntityState()
		entityEvent2.SetEntityType(newObj.EntityType)

		attrs2 := state2.Attributes()
		for k, v := range newObj.Metadata {
			attrs2.PutStr(k, v)
		}
	}

	return out
}
