// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package experimentalmetricmetadata // import "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/experimentalmetricmetadata"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
)

// See entity event design document:
// https://docs.google.com/document/d/1Tg18sIck3Nakxtd3TFFcIjrmRO_0GLMdHXylVqBQmJA/edit#heading=h.pokdp8i2dmxy

const (
	semconvOtelEntityEventAsScope = "otel.entity.event_as_log"

	semconvOtelEntityEventName    = "otel.entity.event.name"
	semconvEventEntityEventState  = "entity_state"
	semconvEventEntityEventDelete = "entity_delete"

	semconvOtelEntityId         = "otel.entity.id"
	semconvOtelEntityType       = "otel.entity.type"
	semconvOtelEntityAttributes = "otel.entity.attributes"
)

type EntityEvents struct {
	orig *entityEventsOrig
}

func NewEntityEvents() EntityEvents {
	return EntityEvents{orig: &entityEventsOrig{}}
}

type entityEventsOrig struct {
	events []entityEventOrig
}

func (e EntityEvents) ToLog() plog.Logs {
	logs := plog.NewLogs()
	scopeLogs := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty()

	scopeLogs.Scope().Attributes().PutBool(semconvOtelEntityEventAsScope, true)

	logRecords := scopeLogs.LogRecords()
	logRecords.EnsureCapacity(len(e.orig.events))

	for _, event := range e.orig.events {
		logRecord := logRecords.AppendEmpty()
		event.toLog(logRecord)
	}

	return logs
}

func (e EntityEvents) Events() EntityEventsSlice {
	return EntityEventsSlice{orig: &e.orig.events}
}

type EntityEventsSlice struct {
	orig *[]entityEventOrig
}

func (s EntityEventsSlice) AppendEmpty() EntityEvent {
	*s.orig = append(
		*s.orig, entityEventOrig{
			id: pcommon.NewMap(),
		},
	)
	return EntityEvent{orig: &(*s.orig)[len(*s.orig)-1]}
}

func (s EntityEventsSlice) Len() int {
	return len(*s.orig)
}

func (s EntityEventsSlice) EnsureCapacity(newCap int) {
	oldCap := cap(*s.orig)
	if newCap <= oldCap {
		return
	}

	newOrig := make([]entityEventOrig, len(*s.orig), newCap)
	copy(newOrig, *s.orig)
	*s.orig = newOrig
}

func (s EntityEventsSlice) At(i int) EntityEvent {
	return EntityEvent{orig: &(*s.orig)[i]}
}

type EntityEvent struct {
	orig *entityEventOrig
}

func (e EntityEvent) Id() pcommon.Map {
	return e.orig.id
}

func (e EntityEvent) SetEmptyEntityState() EntityState {
	e.orig.details = &entityStateOrig{
		attributes: pcommon.NewMap(),
	}
	return e.EntityState()
}

func (e EntityEvent) EntityState() EntityState {
	return EntityState{orig: e.orig.details.(*entityStateOrig)}
}

type DetailsType int

const (
	EventTypeNone = iota
	EventTypeState
	EventTypeDelete
)

func (e EntityEvent) EventType() DetailsType {
	switch e.orig.details.(type) {
	case entityStateOrig:
		return EventTypeState
	case entityDeleteOrig:
		return EventTypeDelete
	default:
		return EventTypeNone
	}
}

func (e EntityEvent) SetEmptyEntityDelete() EntityDelete {
	e.orig.details = &entityDeleteOrig{}
	return e.EntityDelete()
}

func (e EntityEvent) EntityDelete() EntityDelete {
	return EntityDelete{orig: e.orig.details.(*entityDeleteOrig)}
}

func (e EntityEvent) fromLog(l plog.LogRecord) {
	eventName, ok := l.Attributes().Get(semconvOtelEntityEventName)
	if !ok {
		return
	}
	switch eventName.Str() {
	case semconvEventEntityEventState:
		e.SetEmptyEntityState()
		e.EntityState().fromLog(l)

	case semconvEventEntityEventDelete:
		e.SetEmptyEntityDelete()

	default:
		// Unknown event name
		return
	}

	attributes, ok := l.Attributes().Get(semconvOtelEntityId)
	if !ok || attributes.Type() != pcommon.ValueTypeMap {
		return
	}
	// TODO: we need a Map.MoveTo function to be efficient here.
	attributes.Map().CopyTo(e.Id())
}

type entityEventOrig struct {
	timestamp pcommon.Timestamp
	id        pcommon.Map
	details   entityEventDetailsOrig
}

type entityEventDetailsOrig interface {
	toLog(log plog.LogRecord)
}

func (e entityEventOrig) toLog(l plog.LogRecord) {
	l.SetTimestamp(e.timestamp)

	id := l.Attributes().PutEmptyMap(semconvOtelEntityId)
	e.id.CopyTo(id)

	e.details.toLog(l)
}

type EntityState struct {
	orig *entityStateOrig
}

func (s EntityState) fromLog(l plog.LogRecord) {
	entityType, ok := l.Attributes().Get(semconvOtelEntityType)
	if !ok || entityType.Type() != pcommon.ValueTypeStr {
		return
	}
	s.SetEntityType(entityType.Str())

	attrs, ok := l.Attributes().Get(semconvOtelEntityAttributes)
	if !ok || attrs.Type() != pcommon.ValueTypeMap {
		return
	}

	// TODO: we need a Map.MoveTo function to be efficient here.
	attrs.Map().CopyTo(s.orig.attributes)
}

func (s EntityState) Attributes() pcommon.Map {
	return s.orig.attributes
}

func (s EntityState) EntityType() string {
	return s.orig.entityType
}

func (s EntityState) SetEntityType(t string) {
	s.orig.entityType = t
}

type entityStateOrig struct {
	entityType string
	attributes pcommon.Map
}

func (e entityStateOrig) toLog(l plog.LogRecord) {
	lrAttrs := l.Attributes()
	lrAttrs.PutStr(semconvOtelEntityEventName, semconvEventEntityEventState)

	lrAttrs.PutStr(semconvOtelEntityType, e.entityType)

	entityAttrs := lrAttrs.PutEmptyMap(semconvOtelEntityAttributes)
	e.attributes.CopyTo(entityAttrs)
}

type EntityDelete struct {
	orig *entityDeleteOrig
}

type entityDeleteOrig struct {
}

func (e entityDeleteOrig) toLog(l plog.LogRecord) {
	l.Attributes().PutStr(semconvOtelEntityEventName, semconvEventEntityEventDelete)
}

func isEntityEventsScopeLogs(logs plog.ScopeLogs) bool {
	attr, ok := logs.Scope().Attributes().Get(semconvOtelEntityEventAsScope)
	if !ok {
		return false
	}
	return attr.Bool()
}

func ExtractEntityEventsFromLogs(ld plog.Logs) (entityEventLogs plog.Logs) {
	entityEventLogs = plog.NewLogs()

	rls := ld.ResourceLogs()

	for i := 0; i < rls.Len(); i++ {
		rl := rls.At(i)
		ills := rl.ScopeLogs()

		entityRls := entityEventLogs.ResourceLogs().AppendEmpty()
		rl.Resource().CopyTo(entityRls.Resource())

		ills.RemoveIf(
			func(logs plog.ScopeLogs) bool {
				if isEntityEventsScopeLogs(logs) {
					logs.MoveTo(entityRls.ScopeLogs().AppendEmpty())
					// Remove entity event logs from the input logs list.
					return true
				}
				// Keep the rest of the logs as is.
				return false
			},
		)
	}

	return entityEventLogs
}

func EntityEventsFromLogs(logs plog.Logs) EntityEvents {
	ee := NewEntityEvents()
	for i := 0; i < logs.ResourceLogs().Len(); i++ {
		rl := logs.ResourceLogs().At(i)
		for j := 0; j < rl.ScopeLogs().Len(); j++ {
			sl := rl.ScopeLogs().At(j)
			ee.Events().EnsureCapacity(sl.LogRecords().Len())
			for k := 0; k < sl.LogRecords().Len(); k++ {
				lr := sl.LogRecords().At(k)
				event := ee.Events().AppendEmpty()
				event.fromLog(lr)
			}
		}
	}
	return ee
}

/*
func metadataUpdateToLog(m *MetadataUpdate, log plog.LogRecord) {
	log.Attributes().PutStr(semconvOtelEntityEvent, semconvEventEntityEventState)

	id := log.Attributes().PutEmptyMap(semconvOtelEntityId)
	id.PutStr(m.ResourceIDKey, string(m.ResourceID))

	changedAttributes := log.Attributes().PutEmptyMap(semconvOtelCollectorEntityChangedAttributes)
	for k, v := range m.MetadataToAdd {
		changedAttributes.PutStr(k, v)
	}
	for k, v := range m.MetadataToUpdate {
		changedAttributes.PutStr(k, v)
	}

	deleted := log.Attributes().PutEmptySlice(semconvOtelCollectorEntityDeleteAttributes)
	for k, _ := range m.MetadataToRemove {
		v := deleted.AppendEmpty()
		v.SetStr(k)
	}
}

func MetadataUpdatesToLogRecords(metadataList []*MetadataUpdate, scopeLogs plog.ScopeLogs) {
	scopeLogs.Scope().Attributes().PutBool(semconvOtelCollectorEntityEvent, true)

	logs := scopeLogs.LogRecords()
	logs.EnsureCapacity(len(metadataList))
	for _, metadata := range metadataList {
		logRecord := logs.AppendEmpty()
		metadataUpdateToLog(metadata, logRecord)
	}
}
*/
