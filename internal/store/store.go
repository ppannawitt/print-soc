package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"

	"socprint/internal/config"
)

type Job struct {
	ID            string
	Username      string
	Host          string
	FileName      string
	PrinterID     string
	Queue         string
	SubmittedAt   time.Time
	State         string
	SpoolerID     string
	PrintSettings string
	Message       string
}

type Store struct{ db *sql.DB }

func Open() (*Store, error) {
	dir, err := config.Directory()
	if err != nil {
		return nil, err
	}
	return OpenAt(dir)
}

func OpenAt(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	databasePath := filepath.Join(dir, "history.sqlite")
	database, err := sql.Open("sqlite", databasePath)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(1)
	statements := []string{
		"PRAGMA journal_mode=WAL",
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY, username TEXT NOT NULL, host TEXT NOT NULL,
			file_name TEXT NOT NULL, printer_id TEXT NOT NULL, queue TEXT NOT NULL,
			submitted_at TEXT NOT NULL, state TEXT NOT NULL, spooler_id TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '')`,
		"CREATE INDEX IF NOT EXISTS jobs_submitted_at ON jobs(submitted_at DESC)",
		`UPDATE jobs SET state='unknown', message='The app exited before it could confirm the submission.' WHERE state='pending'`,
	}
	for _, statement := range statements {
		if _, err := database.Exec(statement); err != nil {
			_ = database.Close()
			return nil, err
		}
	}
	columns, err := database.Query("PRAGMA table_info(jobs)")
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	hasOptions := false
	for columns.Next() {
		var cid, notnull, primary int
		var name, typ string
		var value any
		if err := columns.Scan(&cid, &name, &typ, &notnull, &value, &primary); err != nil {
			_ = columns.Close()
			_ = database.Close()
			return nil, err
		}
		if name == "print_settings" {
			hasOptions = true
		}
	}
	err = columns.Err()
	_ = columns.Close()
	if err != nil {
		_ = database.Close()
		return nil, err
	}
	if !hasOptions {
		if _, err := database.Exec("ALTER TABLE jobs ADD COLUMN print_settings TEXT NOT NULL DEFAULT ''"); err != nil {
			_ = database.Close()
			return nil, err
		}
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Chmod(databasePath+suffix, 0600); err != nil && !os.IsNotExist(err) {
			_ = database.Close()
			return nil, err
		}
	}
	if err := os.Chmod(databasePath, 0600); err != nil {
		_ = database.Close()
		return nil, err
	}
	return &Store{db: database}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) AddPending(ctx context.Context, job Job) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO jobs(id,username,host,file_name,printer_id,queue,submitted_at,state,message,print_settings)
		VALUES(?,?,?,?,?,?,?,?,?,?)`, job.ID, job.Username, job.Host, job.FileName, job.PrinterID, job.Queue, job.SubmittedAt.UTC().Format(time.RFC3339Nano), "pending", "Submission has not yet been confirmed.", job.PrintSettings)
	return err
}

func (s *Store) Finish(ctx context.Context, id, state, spoolerID, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE jobs SET state=?, spooler_id=?, message=? WHERE id=?`, state, spoolerID, message, id)
	return err
}

func (s *Store) List(ctx context.Context, limit int) ([]Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,host,file_name,printer_id,queue,submitted_at,state,spooler_id,message,print_settings FROM jobs ORDER BY submitted_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var job Job
		var submitted string
		if err := rows.Scan(&job.ID, &job.Username, &job.Host, &job.FileName, &job.PrinterID, &job.Queue, &submitted, &job.State, &job.SpoolerID, &job.Message, &job.PrintSettings); err != nil {
			return nil, err
		}
		job.SubmittedAt, err = time.Parse(time.RFC3339Nano, submitted)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) Pending(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,host,file_name,printer_id,queue,submitted_at,state,spooler_id,message,print_settings FROM jobs WHERE state='pending'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var job Job
		var submitted string
		if err := rows.Scan(&job.ID, &job.Username, &job.Host, &job.FileName, &job.PrinterID, &job.Queue, &submitted, &job.State, &job.SpoolerID, &job.Message, &job.PrintSettings); err != nil {
			return nil, err
		}
		job.SubmittedAt, _ = time.Parse(time.RFC3339Nano, submitted)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) Uncertain(ctx context.Context) ([]Job, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,username,host,file_name,printer_id,queue,submitted_at,state,spooler_id,message,print_settings FROM jobs WHERE state IN ('pending','unknown')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		var job Job
		var submitted string
		if err := rows.Scan(&job.ID, &job.Username, &job.Host, &job.FileName, &job.PrinterID, &job.Queue, &submitted, &job.State, &job.SpoolerID, &job.Message, &job.PrintSettings); err != nil {
			return nil, err
		}
		job.SubmittedAt, _ = time.Parse(time.RFC3339Nano, submitted)
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
