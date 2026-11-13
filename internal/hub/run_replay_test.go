package hub

import (
	"bytes"
	"testing"
)

func TestCoalesceChunksBoundsReplay(t *testing.T) {
	const chunks = 2000
	payload := bytes.Repeat([]byte("x"), 1024)
	var input []runChunk
	for i := 0; i < chunks; i++ {
		input = append(input, runChunk{seq: uint64(i + 1), data: payload})
	}
	out := coalesceChunks(input, 0, 64<<10)
	if len(out) > chunks/32 {
		t.Fatalf("replay was not coalesced: %d messages for %d chunks", len(out), chunks)
	}
	var total int
	for i, chunk := range out {
		total += len(chunk.data)
		if i > 0 && chunk.seq <= out[i-1].seq {
			t.Fatalf("sequence numbers are not increasing: %d after %d", chunk.seq, out[i-1].seq)
		}
	}
	if total != chunks*len(payload) {
		t.Fatalf("coalescing lost data: %d bytes, want %d", total, chunks*len(payload))
	}
	if out[len(out)-1].seq != chunks {
		t.Fatalf("last sequence = %d, want %d", out[len(out)-1].seq, chunks)
	}
	if resumed := coalesceChunks(input, 1990, 64<<10); len(resumed) != 1 || resumed[0].seq != chunks {
		t.Fatalf("resume from last_seq kept old chunks: %+v", resumed)
	}
	if none := coalesceChunks(input, chunks, 64<<10); len(none) != 0 {
		t.Fatalf("fully replayed run replayed again: %+v", none)
	}
}
