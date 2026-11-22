package http_test

import (
	"bufio"
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	appevent "easycode/internal/application/event"
	"easycode/internal/infrastructure/oidc/oidctest"
	"easycode/internal/infrastructure/postgres"
)

// sse.md section 3 (US-48, US-02 CA-7): /me/events carries the domain events of the projects of the
// person, the SSE id is the global outbox position, and member.role_assigned reaches only the lead
// and the person who got the role.
func TestNoticesStreamByGlobalPositionAndAudience(t *testing.T) {
	shutdown := make(chan struct{})
	live := newLiveAPI(t, liveRuntimeDeps(appevent.NewBroker(), shutdown))
	anaAPI := live.as(t, oidctest.User{Subject: "ana", Name: "Ana Souza"})
	carla := live.as(t, oidctest.User{Subject: "carla", Name: "Carla Dias"})
	projectID := createProject(t, anaAPI, "Projeto com avisos")
	base := "/api/v1/projects/" + projectID
	anaAPI.json(t, "PUT", base+"/members/carla", `{"roles":["member"]}`, nil, 200)
	anaAPI.json(t, "PUT", base+"/members/bruno", `{"roles":["approver"]}`, nil, 404) // never signed in, not in the directory
	live.idp.AddUser(oidctest.User{Subject: "bruno", Name: "Bruno Lima"})
	anaAPI.json(t, "PUT", base+"/members/bruno", `{"roles":["approver"]}`, nil, 200)
	if _, err := postgres.NewOutboxPublisher(live.db.PoolAs("easycode_worker")).PublishPending(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	last := int64(count(t, live.db, "SELECT max(global_position) FROM easycode.events"))
	if count(t, live.db, "SELECT count(*) FROM easycode.events WHERE global_position IS NULL") != 0 || last < 3 {
		t.Fatal("every event published with a position")
	}
	lead := openNotices(t, anaAPI, "/api/v1/me/events?since=0")
	member := openNotices(t, carla, "/api/v1/me/events?since=0")
	time.Sleep(3 * time.Second)
	close(shutdown)
	leadFrames, memberFrames := lead(), member()
	if !strings.Contains(memberFrames, "br.easycode.project.created.v1") || !strings.Contains(memberFrames, `"subject":"members/carla"`) {
		t.Fatalf("carla receives the project events and her own role: %s", memberFrames)
	}
	if strings.Contains(memberFrames, `"subject":"members/bruno"`) {
		t.Fatalf("a member does not receive the role of another person: %s", memberFrames)
	}
	if !strings.Contains(leadFrames, `"subject":"members/bruno"`) || !strings.Contains(leadFrames, "id: "+strconv.FormatInt(last, 10)+"\n") {
		t.Fatalf("the lead receives every role change; the SSE id is the global outbox position: %s", leadFrames)
	}
}

// openNotices opens a stream and returns a function that gives what arrived until the stream ended.
func openNotices(t *testing.T, l liveAPI, path string) func() string {
	t.Helper()
	rc := l.send(fasthttp.MethodGet, path, "", nil)
	if rc.Response.StatusCode() != fasthttp.StatusOK {
		t.Fatalf("stream: %d %s", rc.Response.StatusCode(), rc.Response.Body())
	}
	var mu sync.Mutex
	var text strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		reader := bufio.NewReader(rc.Response.BodyStream())
		for {
			line, err := reader.ReadString('\n')
			mu.Lock()
			text.WriteString(line)
			mu.Unlock()
			if err != nil || strings.Contains(line, "easycode.ended") {
				return
			}
		}
	}()
	return func() string {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
		mu.Lock()
		defer mu.Unlock()
		return text.String()
	}
}
