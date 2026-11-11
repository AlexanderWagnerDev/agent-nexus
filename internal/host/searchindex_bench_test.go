package host

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// seedIndexDocuments writes synthetic rows directly so the benchmark measures
// query performance rather than transcript parsing.
func seedIndexDocuments(tb testing.TB, x *SearchIndex, n int) {
	tb.Helper()
	ctx := context.Background()
	tx, err := x.db.BeginTx(ctx, nil)
	if err != nil {
		tb.Fatal(err)
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO documents(agent,native_id,title,cwd,updated_at,fingerprint) VALUES(?,?,?,?,?,?)`)
	if err != nil {
		tb.Fatal(err)
	}
	defer stmt.Close()
	fts, err := tx.PrepareContext(ctx, `INSERT INTO documents_fts(rowid,body,agent,native_id) VALUES(?,?,?,?)`)
	if err != nil {
		tb.Fatal(err)
	}
	defer fts.Close()
	now := time.Now().UTC().UnixMilli()
	for i := 0; i < n; i++ {
		res, err := stmt.ExecContext(ctx, "opencode", fmt.Sprintf("session-%d", i),
			fmt.Sprintf("Session %d", i), "/work/demo", now, "seed")
		if err != nil {
			tb.Fatal(err)
		}
		id, _ := res.LastInsertId()
		body := fmt.Sprintf("session %d about the upload retry loop and pagination in the customers endpoint", i)
		if i%100 == 0 {
			body += " unique-marker"
		}
		if _, err := fts.ExecContext(ctx, id, body, "opencode", fmt.Sprintf("session-%d", i)); err != nil {
			tb.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		tb.Fatal(err)
	}
}

func TestSearchIndex10kPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("10k fixture is skipped in short mode")
	}
	x, err := OpenSearchIndex(filepath.Join(t.TempDir(), "search-index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer x.Close()
	start := time.Now()
	seedIndexDocuments(t, x, 10000)
	build := time.Since(start)
	if count, _ := x.Count(); count != 10000 {
		t.Fatalf("seed count: %d", count)
	}
	var durations []time.Duration
	for i := 0; i < 50; i++ {
		start := time.Now()
		hits, err := x.Search(context.Background(), "upload retry", 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(hits) == 0 {
			t.Fatal("seed query returned no hits")
		}
		durations = append(durations, time.Since(start))
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[len(durations)*95/100]
	t.Logf("10k index: seed %s, search p95 %s", build, p95)
	if build > 60*time.Second {
		t.Fatalf("seeding 10k sessions took %s, budget is 60s", build)
	}
	if p95 > 2*time.Second {
		t.Fatalf("search p95 %s exceeds the 2s budget", p95)
	}
}

func BenchmarkSearchIndexQuery10k(b *testing.B) {
	x, err := OpenSearchIndex(filepath.Join(b.TempDir(), "search-index.db"))
	if err != nil {
		b.Fatal(err)
	}
	defer x.Close()
	seedIndexDocuments(b, x, 10000)
	queries := []string{"upload retry", "pagination customers", "unique-marker", "session 42"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := x.Search(context.Background(), queries[i%len(queries)], 50); err != nil {
			b.Fatal(err)
		}
	}
}
