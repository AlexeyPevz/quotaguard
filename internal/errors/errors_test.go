package errors

import (
	"errors"
	"strings"
	"testing"
)

func TestConfigErrors(t *testing.T) {
	notFound := &ErrConfigNotFound{Path: "/tmp/config.yaml"}
	if !strings.Contains(notFound.Error(), "config file not found") {
		t.Fatalf("unexpected error message: %s", notFound.Error())
	}
	if !strings.Contains(notFound.Error(), notFound.Path) {
		t.Fatalf("expected path in error message: %s", notFound.Error())
	}

	base := errors.New("bad yaml")
	parse := &ErrConfigParse{Err: base}
	if !strings.Contains(parse.Error(), "failed to parse YAML") {
		t.Fatalf("unexpected parse message: %s", parse.Error())
	}
	if !errors.Is(parse, base) {
		t.Fatalf("expected unwrap to base error")
	}

	validation := &ErrConfigValidation{Err: base}
	if !strings.Contains(validation.Error(), "config validation failed") {
		t.Fatalf("unexpected validation message: %s", validation.Error())
	}
	if !errors.Is(validation, base) {
		t.Fatalf("expected unwrap to base error")
	}
}

func TestDatabaseErrors(t *testing.T) {
	base := errors.New("db")

	op := &ErrDatabaseOpen{Path: "/tmp/db.sqlite", Err: base}
	if !strings.Contains(op.Error(), "failed to open database") {
		t.Fatalf("unexpected open message: %s", op.Error())
	}
	if !errors.Is(op, base) {
		t.Fatalf("expected unwrap to base error")
	}

	migration := &ErrDatabaseMigration{Version: 2, Err: base}
	if !strings.Contains(migration.Error(), "database migration 2 failed") {
		t.Fatalf("unexpected migration message: %s", migration.Error())
	}
	if !errors.Is(migration, base) {
		t.Fatalf("expected unwrap to base error")
	}

	query := &ErrDatabaseQuery{Operation: "select", Err: base}
	if !strings.Contains(query.Error(), "database query failed") {
		t.Fatalf("unexpected query message: %s", query.Error())
	}
	if !errors.Is(query, base) {
		t.Fatalf("expected unwrap to base error")
	}
}

func TestOtherErrors(t *testing.T) {
	base := errors.New("boom")

	quota := &ErrQuotaValidation{Field: "limit", AccountID: "acc", Err: base}
	if !strings.Contains(quota.Error(), "quota validation error") {
		t.Fatalf("unexpected quota message: %s", quota.Error())
	}
	if !errors.Is(quota, base) {
		t.Fatalf("expected unwrap to base error")
	}

	start := &ErrServerStart{Addr: ":8080", Err: base}
	if !strings.Contains(start.Error(), "failed to start server") {
		t.Fatalf("unexpected server start message: %s", start.Error())
	}
	if !errors.Is(start, base) {
		t.Fatalf("expected unwrap to base error")
	}

	shutdown := &ErrServerShutdown{Err: base}
	if !strings.Contains(shutdown.Error(), "server shutdown failed") {
		t.Fatalf("unexpected server shutdown message: %s", shutdown.Error())
	}
	if !errors.Is(shutdown, base) {
		t.Fatalf("expected unwrap to base error")
	}

	mkdir := &ErrDirectoryCreate{Path: "/tmp/dir", Err: base}
	if !strings.Contains(mkdir.Error(), "failed to create directory") {
		t.Fatalf("unexpected mkdir message: %s", mkdir.Error())
	}
	if !errors.Is(mkdir, base) {
		t.Fatalf("expected unwrap to base error")
	}

	read := &ErrFileRead{Path: "/tmp/file", Err: base}
	if !strings.Contains(read.Error(), "failed to read file") {
		t.Fatalf("unexpected read message: %s", read.Error())
	}
	if !errors.Is(read, base) {
		t.Fatalf("expected unwrap to base error")
	}
}

func TestErrNoSuitableAccounts(t *testing.T) {
	err := (&ErrNoSuitableAccounts{Reason: "all exhausted"}).Error()
	if !strings.Contains(err, "all exhausted") {
		t.Fatalf("expected reason in error message: %s", err)
	}

	err = (&ErrNoSuitableAccounts{}).Error()
	if err != "no suitable accounts found" {
		t.Fatalf("unexpected message: %s", err)
	}
}
