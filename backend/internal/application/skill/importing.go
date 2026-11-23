package skill

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"easycode/internal/application/audit"
	"easycode/internal/application/transaction"
	domainaudit "easycode/internal/domain/audit"
	"easycode/internal/domain/failure"
	"easycode/internal/domain/identity"
	domainskill "easycode/internal/domain/skill"
)

// Import is a request to import an organization skill (SkillImport).
type Import struct {
	ID            string
	State         string // received | refused | version_created
	Origin        string // upload | git_repository
	ReceivedAt    time.Time
	Skill         string
	Version       int
	VersionStatus string
	BlockReasons  []string
	RefusalReason string
}

// Import states (skill_imports.state).
const (
	ImportReceived       = "received"
	ImportRefused        = "refused"
	ImportVersionCreated = "version_created"
)

// Imports stores the import requests and the human transitions of the machine skill (api role).
type Imports interface {
	// Receive records an upload in received with its package; key is the Idempotency-Key.
	Receive(ctx context.Context, principal identity.Principal, pkg []byte, key string) (Import, error)
	Get(ctx context.Context, principal identity.Principal, id string) (Import, error)
	// VersionState returns the status and block reasons of a version of an organization skill (not-found if none).
	VersionState(ctx context.Context, principal identity.Principal, name string, version int) (string, []string, error)
	Unblock(ctx context.Context, principal identity.Principal, name string, version int, justification string) error
	Deactivate(ctx context.Context, principal identity.Principal, name string, version int, justification string) error
	// NativeName reports whether the native catalog already has the name (an organization skill cannot shadow it).
	NativeName(ctx context.Context, name string) (bool, error)
}

// Importer is the api side of US-44: only the administrator of the organization imports, unblocks or
// deactivates (RNF-13: every step audited).
type Importer struct {
	tx      transaction.Runner
	imports Imports
	library Library
	trail   audit.Trail
	yaml    func([]byte) ([]byte, error)
}

// NewImporter builds the use cases; yaml turns the front matter into JSON.
func NewImporter(tx transaction.Runner, imports Imports, library Library, trail audit.Trail, yaml func([]byte) ([]byte, error)) *Importer {
	return &Importer{tx: tx, imports: imports, library: library, trail: trail, yaml: yaml}
}

func administrator(principal identity.Principal) error {
	if !principal.Administrator {
		return failure.New(failure.KindForbidden, failure.CodeForbidden, nil)
	}
	return nil
}

// Upload is importSkill with a zipped folder: above the transport limit → 413 too-large-package with the limit
// (CA-3); without SKILL.md, name or description → 400 invalid-skill-package with the reason (CA-2), no version.
// Otherwise the request is received and the worker creates the version and scans it.
func (i *Importer) Upload(ctx context.Context, principal identity.Principal, pkg []byte, key string) (Import, error) {
	if err := administrator(principal); err != nil {
		return Import{}, err
	}
	if len(pkg) > domainskill.MaxPackageBytes {
		return Import{}, failure.New(failure.KindInvalidInput, domainskill.CodeTooLargePackage, nil).With("limit_bytes", domainskill.MaxPackageBytes)
	}
	parsed, _, err := domainskill.ParseArchive(pkg, i.yaml)
	var invalid domainskill.Invalid
	if errors.As(err, &invalid) {
		return Import{}, failure.New(failure.KindInvalidInput, domainskill.CodeInvalidPackage, nil).With("reason", invalid.Reason)
	}
	if err != nil {
		return Import{}, err
	}
	native, err := i.imports.NativeName(ctx, parsed.Name)
	if err != nil {
		return Import{}, err
	}
	if native {
		return Import{}, failure.New(failure.KindInvalidInput, domainskill.CodeInvalidPackage, nil).With("reason", "name_of_native_skill")
	}
	var out Import
	err = i.tx.InTx(ctx, func(ctx context.Context) error {
		received, err := i.imports.Receive(ctx, principal, pkg, key)
		if err != nil {
			return err
		}
		out = received
		_, err = i.trail.Append(ctx, domainaudit.Record{
			OrganizationID: principal.OrganizationID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
			Origin: domainaudit.OriginWeb, Action: "skill.import", Target: "skills/" + parsed.Name, Decision: "upload",
		})
		return err
	})
	return out, err
}

// ImportStatus is getSkillImport.
func (i *Importer) ImportStatus(ctx context.Context, principal identity.Principal, id string) (Import, error) {
	if err := administrator(principal); err != nil {
		return Import{}, err
	}
	return i.imports.Get(ctx, principal, id)
}

// Unblock is unblockSkill (CA-4): only a version blocked only by suspicious content is released, with the
// justification; format or size is fixed by sending a new version (409 state-conflict).
func (i *Importer) Unblock(ctx context.Context, principal identity.Principal, name string, version int, justification string) (Summary, error) {
	return i.transition(ctx, principal, name, version, justification, "skill.unblock", func(status string, reasons []string) bool {
		return status == domainskill.StatusBlocked && slices.Equal(reasons, []string{domainskill.ReasonMalicious})
	}, i.imports.Unblock)
}

// Deactivate is deactivateSkill: an unblocked or blocked version of the organization leaves the library.
func (i *Importer) Deactivate(ctx context.Context, principal identity.Principal, name string, version int, justification string) (Summary, error) {
	return i.transition(ctx, principal, name, version, justification, "skill.deactivate", func(status string, _ []string) bool {
		return status == domainskill.StatusUnblocked || status == domainskill.StatusBlocked
	}, i.imports.Deactivate)
}

func (i *Importer) transition(ctx context.Context, principal identity.Principal, name string, version int, justification, action string,
	allowed func(string, []string) bool, apply func(context.Context, identity.Principal, string, int, string) error) (Summary, error) {
	if err := administrator(principal); err != nil {
		return Summary{}, err
	}
	justification = strings.TrimSpace(justification)
	if n := len([]rune(justification)); n < 10 || n > 2000 {
		return Summary{}, failure.Invalid(failure.FieldError{Field: "justification", Message: "length"})
	}
	err := i.tx.InTx(ctx, func(ctx context.Context) error {
		status, reasons, err := i.imports.VersionState(ctx, principal, name, version)
		if err != nil {
			return err
		}
		if !allowed(status, reasons) {
			return failure.New(failure.KindConflict, failure.CodeStateConflict, nil).With("current_state", status).With("available_actions", []string{})
		}
		if err := apply(ctx, principal, name, version, justification); err != nil {
			return err
		}
		_, err = i.trail.Append(ctx, domainaudit.Record{
			OrganizationID: principal.OrganizationID, Actor: domainaudit.Actor{Type: domainaudit.ActorHuman, ID: principal.Subject},
			Origin: domainaudit.OriginWeb, Action: action, Target: "skills/" + name + "/versions/" + strconv.Itoa(version),
			Justification: justification,
		})
		return err
	})
	if err != nil {
		return Summary{}, err
	}
	return i.library.Get(ctx, principal.OrganizationID, name)
}

// Received is an upload waiting for the worker.
type Received struct {
	ID             int64
	OrganizationID int64
	RequestedBy    int64
	Package        []byte
}

// Processor stores what the worker job does (worker role).
type Processor interface {
	// Pending returns the uploads in received, oldest first.
	Pending(ctx context.Context, limit int) ([]Received, error)
	// CreateVersion creates the skill when new and its next version in imported, runs the scan transitions
	// (scanning, then unblocked or blocked) and links the import (version_created, package cleared).
	CreateVersion(ctx context.Context, received Received, pkg domainskill.Package, scan domainskill.Scan, stages []string) (int, error)
	// Refuse closes an import whose package no longer parses (refused, package cleared).
	Refuse(ctx context.Context, received Received, reason string) error
}

// ProcessImports is the worker job of US-44: each received upload becomes a version scanned for format, size
// and malicious content (policy.imported_skill); a suspicious one stays blocked until an administrator
// releases it (CA-4). The skill is data, never an order of the system (RN-1).
func ProcessImports(ctx context.Context, tx transaction.Runner, processor Processor, profiles Profiles, yaml func([]byte) ([]byte, error)) (int, error) {
	pending, err := processor.Pending(ctx, 10)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, received := range pending {
		pkg, unsafe, err := domainskill.ParseArchive(received.Package, yaml)
		err = tx.InTx(ctx, func(ctx context.Context) error {
			var invalid domainskill.Invalid
			if errors.As(err, &invalid) {
				return processor.Refuse(ctx, received, invalid.Reason)
			}
			if err != nil {
				return err
			}
			agents := slices.DeleteFunc(slices.Clone(pkg.Agents), func(a string) bool { return !slices.Contains(profiles.Agents(), a) })
			if len(agents) == 0 {
				agents = profiles.Agents()
			}
			slices.Sort(agents)
			pkg.Agents = agents
			var stages []string
			for _, agent := range agents {
				profile, err := profiles.Profile(agent)
				if err != nil {
					return err
				}
				for _, stage := range profile.Stages {
					if !slices.Contains(stages, stage) {
						stages = append(stages, stage)
					}
				}
			}
			_, err := processor.CreateVersion(ctx, received, pkg, domainskill.ScanPackage(pkg, unsafe, domainskill.DefaultMaxSizeTokens), stages)
			return err
		})
		if err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
