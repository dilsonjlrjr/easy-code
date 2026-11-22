package oidc

import (
	"context"
	"time"

	appproject "easycode/internal/application/project"
	"easycode/internal/application/ssosync"
)

// SSODirectory implements ssosync.Directory over the admin API.
type SSODirectory struct {
	admin *Admin
}

var _ ssosync.Directory = SSODirectory{}

// NewSSODirectory builds the directory of the worker job.
func NewSSODirectory(admin *Admin) SSODirectory { return SSODirectory{admin: admin} }

// Events implements ssosync.Directory.
func (d SSODirectory) Events(ctx context.Context, types []string, since time.Time, first, limit int) ([]ssosync.Event, error) {
	events, err := d.admin.Events(ctx, types, since, first, limit)
	if err != nil {
		return nil, err
	}
	out := make([]ssosync.Event, 0, len(events))
	for _, e := range events {
		out = append(out, ssosync.Event(e))
	}
	return out, nil
}

// Exists implements ssosync.Directory.
func (d SSODirectory) Exists(ctx context.Context, subject string) (bool, error) {
	_, found, err := d.admin.User(ctx, subject)
	return found, err
}

// PeopleDirectory implements appproject.Directory over the admin API (searchUsers, US-02). Only
// the opaque id and the display name leave the realm.
type PeopleDirectory struct {
	admin *Admin
}

var _ appproject.Directory = PeopleDirectory{}

// NewPeopleDirectory builds the directory of the api.
func NewPeopleDirectory(admin *Admin) PeopleDirectory { return PeopleDirectory{admin: admin} }

// Search implements appproject.Directory (enabled accounts only).
func (d PeopleDirectory) Search(ctx context.Context, text string, first, limit int) ([]appproject.UserRef, error) {
	found, err := d.admin.SearchUsers(ctx, text, first, limit)
	if err != nil {
		return nil, err
	}
	out := make([]appproject.UserRef, 0, len(found))
	for _, u := range found {
		if u.Enabled {
			out = append(out, appproject.UserRef{Subject: u.Subject, Name: u.Name})
		}
	}
	return out, nil
}

// Person implements appproject.Directory.
func (d PeopleDirectory) Person(ctx context.Context, subject string) (string, bool, error) {
	u, found, err := d.admin.User(ctx, subject)
	if err != nil || !found || !u.Enabled {
		return "", false, err
	}
	return u.Name, true, nil
}
