package experimentalmetricmetadata

import (
	"bufio"
	"os"
	"testing"

	"go.opentelemetry.io/collector/pdata/plog"
)

func loadEntities() []plog.Logs {
	f, err := os.Open("testdata/k8s.entities.jsonl")
	if err != nil {
		panic(err)
	}
	u := plog.JSONUnmarshaler{}
	scanner := bufio.NewScanner(f)
	var allLogs []plog.Logs
	for scanner.Scan() {
		logs, err := u.UnmarshalLogs([]byte(scanner.Text()))
		if err != nil {
			panic(err)
		}
		allLogs = append(allLogs, logs)
	}
	return allLogs
}

func BenchmarkEventsToLogs(b *testing.B) {
	allLogs := loadEntities()
	var events []EntityEvents
	for _, logs := range allLogs {
		events = append(events, EntityEventsFromLogs(logs))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, e := range events {
			e.ToLog()
		}
	}
}

func BenchmarkEventsFromLogs(b *testing.B) {
	allLogs := loadEntities()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, logs := range allLogs {
			EntityEventsFromLogs(logs)
		}
	}
}
