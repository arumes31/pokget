package catalog

import (
	"context"
	"errors"
	"testing"
)

type syncTestProvider struct {
	records []CardRecord
	err     error
}

func (p syncTestProvider) ID() string { return "source" }
func (p syncTestProvider) Game() Game { return GamePokemon }
func (p syncTestProvider) Fetch(_ context.Context, _ FetchRequest, emit func(CardRecord) error) (FetchResult, error) {
	for _, record := range p.records {
		if err := emit(record); err != nil {
			return FetchResult{}, err
		}
	}
	return FetchResult{Count: int64(len(p.records)), CompleteSnapshot: true}, p.err
}

type syncTestRepository struct {
	batches   []Batch
	failed    bool
	completed bool
}

func (r *syncTestRepository) BeginRun(context.Context, string, SyncMode) (string, error) {
	return "run", nil
}
func (r *syncTestRepository) UpsertBatch(_ context.Context, batch Batch) (ChangeCounts, error) {
	copyBatch := batch
	copyBatch.Records = append([]CardRecord(nil), batch.Records...)
	r.batches = append(r.batches, copyBatch)
	return ChangeCounts{CardsInserted: int64(len(batch.Records))}, nil
}
func (r *syncTestRepository) CompleteRun(context.Context, string, Completion) (ChangeCounts, error) {
	r.completed = true
	return ChangeCounts{CardsInserted: 3}, nil
}
func (r *syncTestRepository) FailRun(context.Context, string, error) error {
	r.failed = true
	return nil
}

func TestSyncerBatchesAndCompletes(t *testing.T) {
	repository := &syncTestRepository{}
	provider := syncTestProvider{records: []CardRecord{{SourceCardID: "1"}, {SourceCardID: "2"}, {SourceCardID: "3"}}}
	completion, err := (&Syncer{Repository: repository, BatchSize: 2}).Sync(context.Background(), provider, SyncModeFull, FetchRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(repository.batches) != 2 || len(repository.batches[0].Records) != 2 || len(repository.batches[1].Records) != 1 {
		t.Fatalf("unexpected batches: %+v", repository.batches)
	}
	if !repository.completed || repository.failed || completion.Changes.CardsInserted != 3 {
		t.Fatalf("unexpected completion=%+v repository=%+v", completion, repository)
	}
}

func TestSyncerMarksFailedRun(t *testing.T) {
	repository := &syncTestRepository{}
	wantErr := errors.New("upstream failed")
	_, err := (&Syncer{Repository: repository}).Sync(context.Background(), syncTestProvider{err: wantErr}, SyncModeFull, FetchRequest{})
	if !errors.Is(err, wantErr) || !repository.failed || repository.completed {
		t.Fatalf("error=%v repository=%+v", err, repository)
	}
}
