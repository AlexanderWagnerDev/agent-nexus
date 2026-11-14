package host

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

const (
	maxAttachmentBytes = 8 << 20
	maxAttachmentCount = 6
	maxAttachmentTotal = 16 << 20
)

type promptFile struct {
	Name string
	MIME string
	Path string
}

// decodedAttachment is one validated upload. The original base64 text is kept
// so a protocol that carries inline base64 (ACP) does not have to re-encode.
type decodedAttachment struct {
	name string
	mime string
	data []byte
	raw  string
}

// decodeAttachments validates count, base64, size and MIME for every upload.
// Agents that forward inline data and agents that stage files share it, so the
// same limits apply on every path.
func decodeAttachments(attachments []protocol.Attachment) ([]decodedAttachment, error) {
	if len(attachments) > maxAttachmentCount {
		return nil, fmt.Errorf("at most %d attachments are allowed", maxAttachmentCount)
	}
	var total int
	out := make([]decodedAttachment, 0, len(attachments))
	for i, attachment := range attachments {
		name := safeAttachmentName(attachment.Name, i+1)
		data, err := base64.StdEncoding.DecodeString(attachment.Data)
		if err != nil {
			return nil, errors.New("attachment is not valid base64")
		}
		if len(data) == 0 {
			return nil, fmt.Errorf("%s is empty", name)
		}
		if len(data) > maxAttachmentBytes {
			return nil, fmt.Errorf("%s exceeds the %d MB attachment limit", name, maxAttachmentBytes>>20)
		}
		if err := validateAttachmentMIME(name, attachment.MIME, data); err != nil {
			return nil, err
		}
		total += len(data)
		if total > maxAttachmentTotal {
			return nil, errors.New("attachments exceed the total size limit")
		}
		out = append(out, decodedAttachment{name: name, mime: attachment.MIME, data: data, raw: attachment.Data})
	}
	return out, nil
}

// validRunID bounds the hub-supplied run identifier. It is used as a
// directory name under the attachment base, so separators, dots and control
// characters are rejected; the hub's own ids are 32 lowercase hex characters.
func validRunID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

func attachmentDir(runID string) string {
	return filepath.Join(os.TempDir(), "agentnexus-attachments", runDirName(runID))
}

// runDirName never lets caller-supplied text escape the attachment base, even
// if a future caller forgets to validate the run id.
func runDirName(runID string) string {
	if validRunID(runID) {
		return runID
	}
	sum := sha256.Sum256([]byte(runID))
	return hex.EncodeToString(sum[:16])
}

// writePromptAttachments materialises browser uploads inside a private
// per-run directory. The hub never writes files; only the host does.
func writePromptAttachments(runID string, attachments []protocol.Attachment) ([]promptFile, error) {
	decoded, err := decodeAttachments(attachments)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 {
		return nil, nil
	}
	dir := attachmentDir(runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := secureDirOwner(dir); err != nil {
		return nil, err
	}
	_ = os.Chmod(dir, 0o700)
	success := false
	defer func() {
		if !success {
			_ = os.RemoveAll(dir)
		}
	}()
	files := make([]promptFile, 0, len(decoded))
	for i, attachment := range decoded {
		name := attachment.name
		ext := filepath.Ext(name)
		if ext == "" && attachment.mime != "" {
			if exts, _ := mime.ExtensionsByType(attachment.mime); len(exts) > 0 {
				ext = exts[0]
				name += ext
			}
		}
		filePath := filepath.Join(dir, fmt.Sprintf("%02d-%s", i+1, name))
		if err := os.WriteFile(filePath, attachment.data, 0o600); err != nil {
			return nil, err
		}
		_ = os.Chmod(filePath, 0o600)
		files = append(files, promptFile{Name: name, MIME: attachment.mime, Path: filePath})
	}
	success = true
	return files, nil
}

// safeAttachmentName keeps only a basename, strips control characters and
// bounds the length. Both separator styles are treated as separators so the
// same payload is safe on Windows, Linux and macOS.
func safeAttachmentName(raw string, index int) string {
	name := strings.ReplaceAll(strings.TrimSpace(raw), "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == 0x202e {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." || name == "/" {
		name = fmt.Sprintf("attachment-%d", index)
	}
	if len(name) > 120 {
		ext := filepath.Ext(name)
		if len(ext) > 16 {
			ext = ""
		}
		name = name[:120-len(ext)] + ext
	}
	return name
}

// validateAttachmentMIME rejects a declared type that contradicts the actual
// bytes. This catches mislabelled executables and broken image uploads before
// they reach an agent. Generic binary uploads remain allowed.
func validateAttachmentMIME(name, declared string, data []byte) error {
	declared = strings.ToLower(strings.TrimSpace(strings.Split(declared, ";")[0]))
	detected := http.DetectContentType(data)
	detectedFamily := strings.SplitN(detected, "/", 2)[0]
	switch {
	case strings.HasPrefix(declared, "image/"):
		// DetectContentType returns application/octet-stream for newer formats
		// such as AVIF/HEIC. Reject content that is clearly text, but do not
		// reject an image only because the sniffer does not know its format.
		if detectedFamily == "text" {
			return fmt.Errorf("%s does not contain valid image data", name)
		}
	case declared == "application/pdf":
		if detected != "application/pdf" {
			return fmt.Errorf("%s does not contain a valid PDF", name)
		}
	case strings.HasPrefix(declared, "text/"):
		if detectedFamily != "text" && detected != "application/pdf" {
			return fmt.Errorf("%s does not contain text data", name)
		}
	}
	return nil
}

// cleanupStaleAttachments removes per-run directories left behind by a crash
// or a forced kill. Runs normally clean their own directory on stop.
func cleanupStaleAttachments(maxAge time.Duration) {
	base := filepath.Join(os.TempDir(), "agentnexus-attachments")
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(base, entry.Name()))
	}
}

func cleanupPromptAttachments(runID string) {
	_ = os.RemoveAll(attachmentDir(runID))
}
