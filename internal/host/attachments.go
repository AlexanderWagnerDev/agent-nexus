package host

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strings"

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

func attachmentDir(runID string) string {
	return filepath.Join(os.TempDir(), "agentnexus-attachments", runID)
}

// writePromptAttachments materialises browser uploads inside a private
// per-run directory. The hub never writes files; only the host does.
func writePromptAttachments(runID string, attachments []protocol.Attachment) ([]promptFile, error) {
	if len(attachments) > maxAttachmentCount {
		return nil, fmt.Errorf("at most %d attachments are allowed", maxAttachmentCount)
	}
	if len(attachments) == 0 {
		return nil, nil
	}
	dir := attachmentDir(runID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	var total int
	files := make([]promptFile, 0, len(attachments))
	for i, attachment := range attachments {
		name := filepath.Base(strings.TrimSpace(attachment.Name))
		if name == "" || name == "." || name == string(filepath.Separator) {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		data, err := base64.StdEncoding.DecodeString(attachment.Data)
		if err != nil {
			return nil, errors.New("attachment is not valid base64")
		}
		if len(data) > maxAttachmentBytes {
			return nil, fmt.Errorf("%s exceeds the %d MB attachment limit", name, maxAttachmentBytes>>20)
		}
		total += len(data)
		if total > maxAttachmentTotal {
			return nil, errors.New("attachments exceed the total size limit")
		}
		ext := filepath.Ext(name)
		if ext == "" && attachment.MIME != "" {
			if exts, _ := mime.ExtensionsByType(attachment.MIME); len(exts) > 0 {
				ext = exts[0]
				name += ext
			}
		}
		path := filepath.Join(dir, fmt.Sprintf("%02d-%s", i+1, name))
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return nil, err
		}
		files = append(files, promptFile{Name: name, MIME: attachment.MIME, Path: path})
	}
	return files, nil
}

func cleanupPromptAttachments(runID string) {
	_ = os.RemoveAll(attachmentDir(runID))
}
