// Package weblink builds the address of the web screen for each item of easycode-desktop. Every action goes
// through the web (PD-07, ADR-20): the app only opens these addresses in the default browser. The interface
// never sends a URL: it sends a Target and the address is built here from the configured server, so the
// app cannot be made to open an arbitrary page.
package weblink

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Kinds of target, mirroring the web routes (project/frontend/src/app/router/routes.ts).
const (
	KindHome                 = "home"
	KindPending              = "pending"
	KindProject              = "project"
	KindProjectConfiguration = "project_configuration"
	KindWave                 = "wave"
	KindGateRound            = "gate_round"
	KindFinding              = "finding"
	KindAccountLocalClient   = "account_local_client"
)

// AccountLocalClientPath is the web page "Minha conta › Cliente local" (ESP-23 section 3.4, PD-08).
// ESP-23 suggests /minha-conta/cliente-local; web routes are in English (ADR-21), so the app uses the
// English form. Keep it equal to the route of project/frontend.
const AccountLocalClientPath = "/account/local-client"

// ErrInvalidTarget is returned for an unknown kind or an identifier outside the expected shape.
var ErrInvalidTarget = errors.New("invalid web target")

// Target names one web screen.
type Target struct {
	Kind      string `json:"kind"`
	ProjectID string `json:"project_id,omitempty"`
	WaveID    string `json:"wave_id,omitempty"`
	GateID    string `json:"gate_id,omitempty"`
	Round     int    `json:"round,omitempty"`
	FindingID string `json:"finding_id,omitempty"`
}

// Identifiers are UUIDs, wave ids like REQ-01, gate ids like gate.requirements and finding ids like ACH-07.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// URL returns the web address of target under server.
func URL(server string, target Target) (string, error) {
	base := strings.TrimRight(server, "/")
	if _, err := url.Parse(base); err != nil || base == "" {
		return "", ErrInvalidTarget
	}
	path, fragment, err := pathOf(target)
	if err != nil {
		return "", err
	}
	return base + path + fragment, nil
}

func pathOf(t Target) (string, string, error) {
	switch t.Kind {
	case KindHome:
		return "/", "", nil
	case KindPending:
		return "/pending", "", nil
	case KindAccountLocalClient:
		return AccountLocalClientPath, "", nil
	}
	if !idPattern.MatchString(t.ProjectID) {
		return "", "", ErrInvalidTarget
	}
	project := "/projects/" + url.PathEscape(t.ProjectID)
	switch t.Kind {
	case KindProject:
		return project, "", nil
	case KindProjectConfiguration:
		return project + "/configuration", "", nil
	case KindWave:
		if !idPattern.MatchString(t.WaveID) {
			return "", "", ErrInvalidTarget
		}
		return project + "/waves/" + url.PathEscape(t.WaveID), "", nil
	case KindGateRound:
		if !idPattern.MatchString(t.GateID) || t.Round < 1 {
			return "", "", ErrInvalidTarget
		}
		return fmt.Sprintf("%s/gates/%s/rounds/%d", project, url.PathEscape(t.GateID), t.Round), "", nil
	case KindFinding:
		if !idPattern.MatchString(t.FindingID) {
			return "", "", ErrInvalidTarget
		}
		// US-32 CA-4: the escalated finding opens on its decision (ESP-20), as in the web Pending items.
		return project + "/findings/" + url.PathEscape(t.FindingID), "#decision", nil
	}
	return "", "", ErrInvalidTarget
}

// ForPendingItem maps a PendingItem of listPendingItems to its web screen, as the web does
// (pendingDestination in project/frontend/src/features/pending-items/api/pendingItemsApi.ts).
func ForPendingItem(itemType, projectID, targetType, targetID string) Target {
	switch targetType {
	case "gate_round":
		if gate, round, ok := strings.Cut(targetID, "/"); ok {
			var number int
			if _, err := fmt.Sscanf(round, "%d", &number); err == nil && number > 0 {
				return Target{Kind: KindGateRound, ProjectID: projectID, GateID: gate, Round: number}
			}
		}
	case "question":
		if wave, _, ok := strings.Cut(targetID, "/"); ok && wave != "" {
			return Target{Kind: KindWave, ProjectID: projectID, WaveID: wave}
		}
	case "finding":
		return Target{Kind: KindFinding, ProjectID: projectID, FindingID: targetID}
	}
	if itemType == "budget" {
		return Target{Kind: KindProjectConfiguration, ProjectID: projectID}
	}
	return Target{Kind: KindProject, ProjectID: projectID}
}
