package catalog

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresRepositoryLeaseImageJobsUsesSkipLocked(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("FOR UPDATE OF image SKIP LOCKED")).
		WithArgs(4, "worker-a", int64(90), "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "card_id", "source_id", "remote_url", "attempts"}).
			AddRow(int64(7), "card-1", "tcgdex", "https://assets.tcgdex.net/card.png", 2))
	mock.ExpectCommit()

	jobs, err := repository.LeaseImageJobs(context.Background(), "worker-a", 4, 90*time.Second)
	if err != nil {
		t.Fatalf("LeaseImageJobs() error = %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != 7 || jobs[0].Attempts != 2 || jobs[0].SourceID != "tcgdex" {
		t.Fatalf("LeaseImageJobs() = %+v", jobs)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryLeaseImageJobsForCardFiltersCard(t *testing.T) {
	t.Parallel()

	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, err := NewPostgresRepository(database)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("AND ($4 = '' OR image.card_id = $4)")).
		WithArgs(2, "targeted-worker", int64(60), "card-7").
		WillReturnRows(sqlmock.NewRows([]string{"id", "card_id", "source_id", "remote_url", "attempts"}))
	mock.ExpectCommit()

	if _, err := repository.LeaseImageJobsForCard(context.Background(), "targeted-worker", "card-7", 2, time.Minute); err != nil {
		t.Fatalf("LeaseImageJobsForCard() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryImageProgress(t *testing.T) {
	t.Parallel()

	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository, err := NewPostgresRepository(database)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectQuery(regexp.QuoteMeta("WITH eligible_images AS MATERIALIZED")).
		WillReturnRows(sqlmock.NewRows([]string{
			"total", "ready", "pending", "processing", "retryable", "failed", "unavailable", "fingerprints",
		}).AddRow(int64(100), int64(30), int64(52), int64(8), int64(5), int64(2), int64(3), int64(90)))

	progress, err := repository.ImageProgress(context.Background())
	if err != nil {
		t.Fatalf("ImageProgress() error = %v", err)
	}
	if progress.Total != 100 || progress.Ready != 30 || progress.Remaining() != 65 || progress.Fingerprints != 90 {
		t.Fatalf("ImageProgress() = %+v", progress)
	}
	if progress.ReadyPercent() != 30 {
		t.Fatalf("ReadyPercent() = %v, want 30", progress.ReadyPercent())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryMarkImageReadyIsTransactional(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	ready := ReadyImage{
		ImageID:       7,
		LeaseOwner:    "worker-a",
		LocalPath:     `C:\images\ab\abcdef.png`,
		ContentSHA256: "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		MIMEType:      "image/png",
		Width:         640,
		Height:        880,
		ByteSize:      1234,
		PHash:         99,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT card_id, lease_owner")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"card_id", "lease_owner"}).AddRow("card-1", "worker-a"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE card_images")).
		WithArgs(int64(7), ready.LocalPath, ready.ContentSHA256, "", "", "image/png", 640, 880, int64(1234)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO card_fingerprints")).
		WithArgs(int64(7), FingerprintAlgorithm, FingerprintAlgorithmVersion, FingerprintTransform, int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE cards SET phash = $2 WHERE id = $1 AND phash IS NULL")).
		WithArgs("card-1", int64(99)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repository.MarkImageReady(context.Background(), ready); err != nil {
		t.Fatalf("MarkImageReady() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryMarkImageReadyRejectsLostLease(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	ready := ReadyImage{
		ImageID:       7,
		LeaseOwner:    "worker-a",
		LocalPath:     "image.png",
		ContentSHA256: "abcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcdefabcd",
		MIMEType:      "image/png",
		Width:         1,
		Height:        1,
		ByteSize:      1,
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta("SELECT card_id, lease_owner")).
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"card_id", "lease_owner"}).AddRow("card-1", "worker-b"))
	mock.ExpectRollback()
	if err := repository.MarkImageReady(context.Background(), ready); !errors.Is(err, ErrImageLeaseLost) {
		t.Fatalf("MarkImageReady() error = %v, want ErrImageLeaseLost", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryMarkImageFailed(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository, err := NewPostgresRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	retryAt := time.Date(2026, time.August, 4, 12, 0, 0, 0, time.UTC)
	mock.ExpectExec(regexp.QuoteMeta("UPDATE card_images")).
		WithArgs(int64(7), "worker-a", "failed", retryAt, "temporary failure").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err = repository.MarkImageFailed(context.Background(), ImageFailure{
		ImageID: 7, LeaseOwner: "worker-a", Kind: ImageFailureRetryable,
		RetryAt: &retryAt, Cause: errors.New("temporary failure"),
	})
	if err != nil {
		t.Fatalf("MarkImageFailed() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRetryDelayCapsWithoutOverflow(t *testing.T) {
	t.Parallel()

	if got := RetryDelay(3, time.Second, time.Hour); got != 4*time.Second {
		t.Fatalf("RetryDelay(3) = %s, want 4s", got)
	}
	if got := RetryDelay(1000, time.Second, time.Hour); got != time.Hour {
		t.Fatalf("RetryDelay(1000) = %s, want 1h", got)
	}
}
