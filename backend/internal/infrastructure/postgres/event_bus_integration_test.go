package postgres_test

import (
	"context"
	"sync"
	"testing"
	"time"

	appevent "easycode/internal/application/event"
	"easycode/internal/domain/event"
	"easycode/internal/domain/failure"
	"easycode/internal/infrastructure/postgres"
	"easycode/internal/infrastructure/postgres/postgrestest"
)

func TestOutboxAssignsGaplessSequencesUnderConcurrency(t *testing.T) {
	db := postgrestest.New(t)
	project := db.CreateProject("outbox-sequence")
	outbox := postgres.NewOutbox(db.PoolAs("easycode_api"))
	ctx := context.Background()

	const writers = 12
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := outbox.Append(ctx, gateApproved(project.ProjectID))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	all, err := outbox.ProjectEvents(ctx, project.ProjectID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != writers {
		t.Fatalf("read %d events, want %d", len(all), writers)
	}
	for i, envelope := range all {
		if envelope.Sequence != int64(i+1) {
			t.Fatalf("event %d has sequence %d", i, envelope.Sequence)
		}
		if err := envelope.Validate(); err != nil {
			t.Fatalf("stored envelope invalid: %v", err)
		}
		if envelope.Source != "/projects/"+project.ProjectID || envelope.DataSchema != "urn:easycode:schema:event:payload-gate:v1#/$defs/approved" {
			t.Fatalf("unexpected envelope %+v", envelope)
		}
	}
	tail, err := outbox.ProjectEvents(ctx, project.ProjectID, writers-2, 100)
	if err != nil || len(tail) != 2 || tail[0].Sequence != writers-1 {
		t.Fatalf("replay after %d: %d events, %v", writers-2, len(tail), err)
	}
}

func TestOutboxRefusesTelemetryAndUnknownProject(t *testing.T) {
	db := postgrestest.New(t)
	project := db.CreateProject("outbox-refuse")
	outbox := postgres.NewOutbox(db.PoolAs("easycode_api"))
	ctx := context.Background()

	telemetry := costRecorded(project.ProjectID)
	if _, err := outbox.Append(ctx, telemetry); failure.From(err).Code != postgres.CodeEventInvalid {
		t.Fatalf("telemetry in the outbox: %v", err)
	}
	if _, err := outbox.Append(ctx, gateApproved("00000000-0000-4000-8000-000000000000")); failure.From(err).Code != postgres.CodeUnknownProject {
		t.Fatalf("unknown project: %v", err)
	}
}

func TestPublisherNotifiesListenersInOrder(t *testing.T) {
	db := postgrestest.New(t)
	project := db.CreateProject("outbox-publish")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	api := db.PoolAs("easycode_api")
	worker := db.PoolAs("easycode_worker")
	broker := appevent.NewBroker()
	subscription := broker.Subscribe(project.ProjectID, 16)
	connected := make(chan struct{}, 1)
	go func() {
		_ = postgres.NewEventListener(api, db.Logger, func(context.Context) { connected <- struct{}{} }).Listen(ctx, broker.Dispatch)
	}()
	outboxWake := make(chan string, 16)
	outboxConnected := make(chan struct{}, 1)
	go func() {
		_ = postgres.NewListener(worker, db.Logger, func(context.Context) { outboxConnected <- struct{}{} }, postgres.ChannelOutbox).
			Run(ctx, func(channel, _ string) { outboxWake <- channel })
	}()
	waitFor(ctx, t, connected)
	waitFor(ctx, t, outboxConnected)

	outbox := postgres.NewOutbox(api)
	for range 2 {
		if _, err := outbox.Append(ctx, gateApproved(project.ProjectID)); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(ctx, t, outboxWake)

	publisher := postgres.NewOutboxPublisher(worker)
	published, err := publisher.PublishPending(ctx, 100)
	if err != nil || published != 2 {
		t.Fatalf("published %d, %v", published, err)
	}
	var last int64
	for want := int64(1); want <= 2; want++ {
		select {
		case n := <-subscription.C():
			if n.Sequence != want || n.Position <= last || n.Telemetry != nil {
				t.Fatalf("notification %+v after position %d", n, last)
			}
			last = n.Position
		case <-ctx.Done():
			t.Fatal("notification not received")
		}
	}
	if again, err := publisher.PublishPending(ctx, 100); err != nil || again != 0 {
		t.Fatalf("second publish %d, %v", again, err)
	}
	if pending := count(t, db, "SELECT count(*) FROM easycode.events WHERE published_at IS NULL"); pending != 0 {
		t.Fatalf("%d events still pending", pending)
	}

	emitted, err := postgres.NewTelemetryNotifier(worker).Emit(ctx, costRecorded(project.ProjectID))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-subscription.C():
		if n.Telemetry == nil || n.Telemetry.ID != emitted.ID || n.Sequence != 0 {
			t.Fatalf("telemetry notification %+v", n)
		}
	case <-ctx.Done():
		t.Fatal("telemetry not received")
	}
}

func costRecorded(projectID string) event.Draft {
	return event.Draft{
		Type: "br.easycode.cost.model_call_recorded.v1", Subject: "waves/REQ-01", ProjectID: projectID, WaveID: "REQ-01",
		CorrelationID: correlation, Data: []byte(`{"wave":"REQ-01","tokens":{"input":10,"output":5,"read_cache":0,"write_cache":0}}`),
	}
}

func waitFor[T any](ctx context.Context, t *testing.T, ch <-chan T) {
	t.Helper()
	select {
	case <-ch:
	case <-ctx.Done():
		t.Fatal("timed out")
	}
}
