package http_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"strings"
	"testing"

	appevent "easycode/internal/application/event"
)

// multipartFile builds the attachFile body with one file part.
func multipartFile(t *testing.T, name string, content []byte) (string, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return body.String(), form.FormDataContentType()
}

// US-03 CA-4, RN-3 and PL-55: a file (a pasted minute enters as a text attachment) is attached while the
// project is created, with the type detected on the server and the trail recorded; above the limit it is
// refused with 413 and the limit; out of created the sources are read-only (409).
func TestAttachFileAndListAttachments(t *testing.T) {
	live := newLiveAPI(t, liveRuntimeDeps(appevent.NewBroker(), make(chan struct{})))
	projectID := createProject(t, live, "Projeto com anexos")
	base := "/api/v1/projects/" + projectID

	body, contentType := multipartFile(t, `C:\atas\ata-reuniao.md`, []byte("# Ata\n\nReservar salas sem conflito.\n"))
	headers := key()
	headers["Content-Type"] = contentType
	created := live.json(t, "POST", base+"/attachments", body, headers, 201)
	if created["file_name"] != "ata-reuniao.md" || created["media_type"] != "text/markdown" || created["size_bytes"] != float64(36) {
		t.Fatalf("attachment: %v", created)
	}
	if by, _ := created["submitted_by"].(map[string]any); by["id"] != "dev" || created["sha256"] == "" {
		t.Fatalf("submitted_by and sha256: %v", created)
	}
	// The same key answers the same attachment (idempotency), without a second record.
	if again := live.json(t, "POST", base+"/attachments", body, headers, 201); again["id"] != created["id"] {
		t.Fatalf("replay: %v", again)
	}

	big, bigType := multipartFile(t, "planilha.csv", bytes.Repeat([]byte("a,b\n"), 20<<10))
	tooLarge := key()
	tooLarge["Content-Type"] = bigType
	refused := live.json(t, "POST", base+"/attachments", big, tooLarge, 413)
	if !strings.HasSuffix(refused["type"].(string), "too-large-attachment") || refused["limit_bytes"] != float64(64<<10) {
		t.Fatalf("413: %v", refused)
	}
	missing := key()
	missing["Content-Type"] = "multipart/form-data; boundary=x"
	live.json(t, "POST", base+"/attachments", "--x--\r\n", missing, 400)

	list := live.json(t, "GET", base+"/attachments", "", nil, 200)["items"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != created["id"] {
		t.Fatalf("listAttachments: %v", list)
	}
	if n := count(t, live.db, "SELECT count(*) FROM easycode.audit_records WHERE action = 'project.attach_file'"); n != 1 {
		t.Fatalf("trail records: %d", n)
	}

	if _, err := live.db.Admin.Exec(context.Background(), "UPDATE easycode.projects SET state = 'canceled', finished_at = now() WHERE public_id = $1::uuid", projectID); err != nil {
		t.Fatal(err)
	}
	late := key()
	late["Content-Type"] = contentType
	if conflict := live.json(t, "POST", base+"/attachments", body, late, 409); !strings.HasSuffix(conflict["type"].(string), "state-conflict") {
		t.Fatalf("out of created: %v", conflict)
	}
}
