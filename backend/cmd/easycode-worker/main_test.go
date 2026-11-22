package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"
)

var testJobs = jobRunner{logger: slog.New(slog.NewTextHandler(io.Discard, nil)), tracer: noop.NewTracerProvider().Tracer("test")}

type fakeMaintainer struct{ calls chan struct{} }

func (f fakeMaintainer) EnsureMonthlyPartitions(context.Context) (int, error) {
	f.calls <- struct{}{}
	return 1, nil
}

func TestPartitionJobRunsAtStartupAndEveryInterval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := make(chan struct{}, 8)
	go runPartitionJob(ctx, testJobs, fakeMaintainer{calls}, 10*time.Millisecond)
	for i := 0; i < 2; i++ {
		select {
		case <-calls:
		case <-time.After(2 * time.Second):
			t.Fatalf("call %d did not happen", i+1)
		}
	}
}

type fakePublisher struct {
	results []int
	calls   chan int
}

func (f *fakePublisher) PublishPending(_ context.Context, limit int) (int, error) {
	published := 0
	if len(f.results) > 0 {
		published, f.results = f.results[0], f.results[1:]
	}
	f.calls <- published
	return min(published, limit), nil
}

func TestOutboxPublisherDrainsFullBatchesAndWakesOnNotify(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publisher := &fakePublisher{results: []int{publishBatch, 3, 0, 1}, calls: make(chan int, 8)}
	wake := make(chan struct{}, 1)
	go runOutboxPublisher(ctx, testJobs, publisher, wake, time.Hour)

	expect := func(want int) {
		t.Helper()
		select {
		case got := <-publisher.calls:
			if got != want {
				t.Fatalf("publish returned %d, want %d", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("publish returning %d did not happen", want)
		}
	}
	expect(publishBatch) // startup run, full batch: drains again at once
	expect(3)
	wake <- struct{}{}
	expect(0)
	wake <- struct{}{}
	expect(1)
}
