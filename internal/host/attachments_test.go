package host

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestSafeAttachmentName(t *testing.T) {
	cases := map[string]string{
		"../../evil.txt":                  "evil.txt",
		"..\\..\\evil.txt":                "evil.txt",
		"dir/sub/report.md":               "report.md",
		"bad\x00name.txt":                 "badname.txt",
		"  spaced name.txt  ":             "spaced name.txt",
		"..":                              "attachment-1",
		"":                                "attachment-1",
		"a/b/":                            "b",
		strings.Repeat("x", 200) + ".txt": strings.Repeat("x", 116) + ".txt",
	}
	for input, want := range cases {
		if got := safeAttachmentName(input, 1); got != want {
			t.Errorf("safeAttachmentName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestValidateAttachmentMIME(t *testing.T) {
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0, 0, 0, 0}
	if err := validateAttachmentMIME("a.png", "image/png", png); err != nil {
		t.Fatalf("valid PNG rejected: %v", err)
	}
	if err := validateAttachmentMIME("a.png", "image/png", []byte("not an image")); err == nil {
		t.Fatal("text disguised as PNG was accepted")
	}
	if err := validateAttachmentMIME("a.pdf", "application/pdf", []byte("%PDF-1.7\n")); err != nil {
		t.Fatalf("valid PDF rejected: %v", err)
	}
	if err := validateAttachmentMIME("a.pdf", "application/pdf", []byte("hello")); err == nil {
		t.Fatal("text disguised as PDF was accepted")
	}
	if err := validateAttachmentMIME("a.txt", "text/plain", []byte("hello")); err != nil {
		t.Fatalf("valid text rejected: %v", err)
	}
	if err := validateAttachmentMIME("a.bin", "application/octet-stream", []byte{0, 1, 2, 3}); err != nil {
		t.Fatalf("generic binary rejected: %v", err)
	}
}

func TestWritePromptAttachmentsRejectsBadPayloads(t *testing.T) {
	runID := "test-bad-attachments"
	t.Cleanup(func() { cleanupPromptAttachments(runID) })
	if _, err := writePromptAttachments(runID, []protocol.Attachment{{Name: "empty.txt", MIME: "text/plain", Data: ""}}); err == nil {
		t.Fatal("empty attachment was accepted")
	}
	badImage := base64.StdEncoding.EncodeToString([]byte("not an image"))
	if _, err := writePromptAttachments(runID, []protocol.Attachment{{Name: "fake.png", MIME: "image/png", Data: badImage}}); err == nil {
		t.Fatal("mislabelled image was accepted")
	}
	if _, err := os.Stat(attachmentDir(runID)); !os.IsNotExist(err) {
		t.Fatal("failed upload left a partial attachment directory")
	}
}

func TestCleanupStaleAttachments(t *testing.T) {
	base := filepath.Join(os.TempDir(), "agentnexus-attachments")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	oldDir := filepath.Join(base, "stale-test-old")
	newDir := filepath.Join(base, "stale-test-new")
	t.Cleanup(func() {
		_ = os.RemoveAll(oldDir)
		_ = os.RemoveAll(newDir)
	})
	for _, dir := range []string{oldDir, newDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(oldDir, old, old); err != nil {
		t.Fatal(err)
	}
	cleanupStaleAttachments(24 * time.Hour)
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("stale attachment directory was not removed")
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatal("fresh attachment directory was removed")
	}
}
