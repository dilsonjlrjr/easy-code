package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeMaintainer struct{ calls chan struct{} }

func (f fakeMaintainer) EnsureMonthlyPartitions(context.Context) (int, error) {
	f.calls <- struct{}{}
	return 1, nil
}

func TestPartitionJobRunsAtStartupAndEveryInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{}, 8)
	go runPartitionJob(ctx, fakeMaintainer{calls}, 10*time.Millisecond, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < 2; i++ {
		select {
		case <-calls:
		case <-time.After(2 * time.Second):
			t.Fatalf("call %d did not happen", i+1)
		}
	}
}
