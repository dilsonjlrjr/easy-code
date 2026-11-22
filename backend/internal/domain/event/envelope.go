// Package event holds the CloudEvents 1.0 envelope of every Easy Code event
// (specs/dominio/eventos/envelope.schema.json). Domain events go through the outbox with a
// per-project sequence; telemetry (agent and cost groups) is best effort, without sequence.
package event

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Fixed envelope attributes.
const (
	SpecVersion     = "1.0"
	DataContentType = "application/json"
	// payloadSchemaVersion is the version of the payload schema groups in specs/dominio/eventos/payloads.
	payloadSchemaVersion = "v1"
)

const uuidPattern = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var (
	typePattern    = regexp.MustCompile(`^br\.easycode\.(project|release|stage|gate|artifact|finding|wave|agent|cost|skill|member)\.([a-z][a-z0-9_]*)\.v[1-9][0-9]*$`)
	waveIDPattern  = regexp.MustCompile(`^(REQ|ARQ|DBA|DSG|VAL|DEV|QA|SEG|REL)-[0-9]{2,}$`)
	uuidRegexp     = regexp.MustCompile(`^` + uuidPattern + `$`)
	idPattern      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	subjectPattern = regexp.MustCompile(`^(project|stages/[a-z][a-z0-9_-]*|gates/gate\.[a-z][a-z0-9_-]*/rounds/[1-9][0-9]*|artifacts/[A-Z][A-Z0-9-]*|findings/ACH-[0-9]{2,}|waves/(REQ|ARQ|DBA|DSG|VAL|DEV|QA|SEG|REL)-[0-9]{2,}|skills/[a-z][a-z0-9-]*/versions/[1-9][0-9]*|members/[A-Za-z0-9._-]{1,128})$`)
	traceParent    = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$`)
)

// Envelope is a CloudEvent in structured JSON mode with the Easy Code extensions.
type Envelope struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Source          string          `json:"source"`
	Type            string          `json:"type"`
	Subject         string          `json:"subject"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	DataSchema      string          `json:"dataschema"`
	Data            json.RawMessage `json:"data"`
	ProjectID       string          `json:"projectid,omitempty"`
	WaveID          string          `json:"waveid,omitempty"`
	CorrelationID   string          `json:"correlationid"`
	CausationID     string          `json:"causationid,omitempty"`
	Sequence        int64           `json:"sequence,omitempty"`
	TraceParent     string          `json:"traceparent,omitempty"`
	TraceState      string          `json:"tracestate,omitempty"`
}

// Draft is what a producer states about a fact; the outbox or the telemetry emitter adds
// the id, the source and (for domain events) the sequence.
type Draft struct {
	Type          string
	Subject       string
	ProjectID     string // public id of the project (projects.public_id)
	WaveID        string
	CorrelationID string
	CausationID   string
	Time          time.Time
	Data          json.RawMessage // thin payload: ids, versions, states, numbers; never content or secrets
	TraceParent   string
	TraceState    string
}

// ErrInvalid marks an envelope that breaks envelope.schema.json.
var ErrInvalid = errors.New("invalid event envelope")

// IsTelemetry reports whether the type is best-effort telemetry (agent and cost groups).
func IsTelemetry(eventType string) bool {
	return strings.HasPrefix(eventType, "br.easycode.agent.") || strings.HasPrefix(eventType, "br.easycode.cost.")
}

// IsOrganizationFlow reports whether the type belongs to the organization flow (skill events, D-26).
func IsOrganizationFlow(eventType string) bool {
	return strings.HasPrefix(eventType, "br.easycode.skill.")
}

// DataSchemaFor derives the dataschema of a type: the payload group $id plus the type pointer.
func DataSchemaFor(eventType string) (string, error) {
	match := typePattern.FindStringSubmatch(eventType)
	if match == nil {
		return "", fmt.Errorf("%w: type %q", ErrInvalid, eventType)
	}
	return fmt.Sprintf("urn:easycode:schema:event:payload-%s:%s#/$defs/%s", match[1], payloadSchemaVersion, match[2]), nil
}

// NewDomain builds a project domain event with its outbox id and sequence.
func NewDomain(draft Draft, id string, sequence int64) (Envelope, error) {
	if IsTelemetry(draft.Type) {
		return Envelope{}, fmt.Errorf("%w: %s is telemetry, not a domain event", ErrInvalid, draft.Type)
	}
	envelope, err := build(draft, id, "/projects/"+draft.ProjectID)
	if err != nil {
		return Envelope{}, err
	}
	envelope.Sequence = sequence
	return envelope, envelope.Validate()
}

// NewTelemetry builds a best-effort telemetry event of a wave (no sequence).
func NewTelemetry(draft Draft, id string) (Envelope, error) {
	if !IsTelemetry(draft.Type) {
		return Envelope{}, fmt.Errorf("%w: %s is not telemetry", ErrInvalid, draft.Type)
	}
	envelope, err := build(draft, id, "/projects/"+draft.ProjectID+"/waves/"+draft.WaveID)
	if err != nil {
		return Envelope{}, err
	}
	return envelope, envelope.Validate()
}

func build(draft Draft, id, source string) (Envelope, error) {
	schema, err := DataSchemaFor(draft.Type)
	if err != nil {
		return Envelope{}, err
	}
	data := draft.Data
	if len(data) == 0 {
		data = json.RawMessage(`{}`)
	}
	return Envelope{
		SpecVersion: SpecVersion, ID: id, Source: source, Type: draft.Type, Subject: draft.Subject,
		Time: draft.Time, DataContentType: DataContentType, DataSchema: schema, Data: data,
		ProjectID: draft.ProjectID, WaveID: draft.WaveID, CorrelationID: draft.CorrelationID,
		CausationID: draft.CausationID, TraceParent: draft.TraceParent, TraceState: draft.TraceState,
	}, nil
}

// Validate checks the envelope rules of envelope.schema.json for project events. Payload
// validation by dataschema belongs to the producer of each type.
func (e Envelope) Validate() error {
	var problems []string
	check := func(ok bool, problem string) {
		if !ok {
			problems = append(problems, problem)
		}
	}
	check(e.SpecVersion == SpecVersion, "specversion")
	check(idPattern.MatchString(e.ID), "id")
	check(typePattern.MatchString(e.Type), "type")
	check(subjectPattern.MatchString(e.Subject), "subject")
	check(!e.Time.IsZero(), "time")
	check(e.DataContentType == DataContentType, "datacontenttype")
	check(json.Valid(e.Data) && strings.HasPrefix(strings.TrimSpace(string(e.Data)), "{"), "data")
	check(idPattern.MatchString(e.CorrelationID), "correlationid")
	check(e.CausationID == "" || idPattern.MatchString(e.CausationID), "causationid")
	check(e.WaveID == "" || waveIDPattern.MatchString(e.WaveID), "waveid")
	check(e.TraceParent == "" || traceParent.MatchString(e.TraceParent), "traceparent")
	check(len(e.TraceState) <= 512, "tracestate")
	check(!IsOrganizationFlow(e.Type), "type: organization flow (skill events) is not supported here")
	check(uuidRegexp.MatchString(e.ProjectID), "projectid")
	if IsTelemetry(e.Type) {
		check(e.WaveID != "", "waveid required on telemetry")
		check(e.Sequence == 0, "sequence forbidden on telemetry")
		check(e.Source == "/projects/"+e.ProjectID+"/waves/"+e.WaveID, "source")
		check(strings.HasPrefix(e.Subject, "waves/"), "subject must be the wave on telemetry")
	} else {
		check(e.Sequence >= 1, "sequence")
		check(e.Source == "/projects/"+e.ProjectID, "source")
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalid, strings.Join(problems, ", "))
	}
	return nil
}
