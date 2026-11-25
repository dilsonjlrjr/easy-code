// Package retention is the worker job of the deletion of a project (US-04, policy.data_retention, RNF-20):
// for every project whose deletion was asked, it destroys the repository of the project (the clone and the
// working copies of its waves) and anonymizes the personal data of the project in one transaction (free
// human texts, content of model calls and checkpoints, the files of the attachments in the files volume), recording data_anonymized_at and the event
// project.data_anonymized. The audit trail and the domain events stay, with opaque ids only (RN-1).
package retention

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"easycode/internal/application/event"
	"easycode/internal/application/transaction"
	domainevent "easycode/internal/domain/event"
)

// Store reads and anonymizes the projects whose deletion was asked.
type Store interface {
	// PendingDeletions lists, oldest first, the projects with the deletion asked and not yet anonymized.
	PendingDeletions(ctx context.Context, limit int) ([]string, error)
	// Anonymize clears the personal data of the project and records data_anonymized_at; it marks the content
	// of the attachments purged (content_purged_at). It runs with the retention role inside the transaction of
	// the context.
	Anonymize(ctx context.Context, projectID string) (Anonymized, error)
}

// Anonymized is what one anonymization changed.
type Anonymized struct {
	Records int      // records changed
	Files   []string // paths in the files volume of the attachments whose content was purged
}

// Files deletes the files of the attachments from the files volume.
type Files interface {
	// Remove deletes a stored file; a missing file is not an error.
	Remove(ctx context.Context, path string) error
}

// Workspaces destroys what the project keeps outside the database.
type Workspaces interface {
	// Remove deletes the repository of the project; a missing repository is not an error.
	Remove(ctx context.Context, projectID string) error
}

// Job runs the anonymization of deleted projects.
type Job struct {
	tx         transaction.Runner
	store      Store
	workspaces Workspaces
	files      Files
	outbox     event.Outbox
	batch      int
}

// NewJob builds the job.
func NewJob(tx transaction.Runner, store Store, workspaces Workspaces, files Files, outbox event.Outbox) *Job {
	return &Job{tx: tx, store: store, workspaces: workspaces, files: files, outbox: outbox, batch: 20}
}

// Run anonymizes the pending projects and returns how many were done.
func (j *Job) Run(ctx context.Context) (int, error) {
	pending, err := j.store.PendingDeletions(ctx, j.batch)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, projectID := range pending {
		if err := j.workspaces.Remove(ctx, projectID); err != nil {
			return done, err
		}
		err := j.tx.InTx(ctx, func(ctx context.Context) error {
			done, err := j.store.Anonymize(ctx, projectID)
			if err != nil {
				return err
			}
			// The files go inside the transaction: a failure rolls the purge mark back and the next pass
			// retries (a file already removed is not an error).
			for _, path := range done.Files {
				if err := j.files.Remove(ctx, path); err != nil {
					return err
				}
			}
			records := done.Records
			data, err := json.Marshal(map[string]any{
				"project": projectID, "actor": map[string]string{"type": "orchestrator", "id": "orchestrator"},
				"cause": "deletion_requested", "anonymized_records": records,
			})
			if err != nil {
				return err
			}
			correlation, err := uuid.NewV7()
			if err != nil {
				return err
			}
			_, err = j.outbox.Append(ctx, domainevent.Draft{
				Type: "br.easycode.project.data_anonymized.v1", Subject: "project", ProjectID: projectID,
				CorrelationID: correlation.String(), Data: data,
			})
			return err
		})
		if err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}
