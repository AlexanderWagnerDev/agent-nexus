package host

import "testing"

func TestOutputBufferIsBoundedAndKeepsNewest(t *testing.T) {
	b := newOutputBuffer(16)
	b.append(1, []byte("aaaaaaaaaa"))
	b.append(2, []byte("bbbbbbbbbb"))
	chunks := b.snapshot()
	if len(chunks) != 1 {
		t.Fatalf("expected one retained chunk, got %d", len(chunks))
	}
	// 20 bytes exceed the limit; the oldest chunk is dropped, the newest is
	// retained with its original sequence.
	if chunks[0].seq != 2 || string(chunks[0].data) != "bbbbbbbbbb" {
		t.Fatalf("oldest chunk was not evicted: %+v", chunks)
	}
}

func TestOutputBufferTruncatesOversizedChunk(t *testing.T) {
	b := newOutputBuffer(4)
	b.append(7, []byte("0123456789"))
	chunks := b.snapshot()
	if len(chunks) != 1 || string(chunks[0].data) != "6789" {
		t.Fatalf("oversized chunk was not truncated: %+v", chunks)
	}
	if b.dropped != 6 {
		t.Fatalf("expected 6 dropped bytes, got %d", b.dropped)
	}
}
