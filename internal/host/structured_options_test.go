package host

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestStructuredRunOptions(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/model", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[
			{"id":"model-a","providerID":"provider-a","name":"Model A","enabled":true,"status":"active","capabilities":{"input":["text","image"]},"variants":[{"id":"low"},{"id":"high"}]},
			{"id":"model-b","providerID":"provider-b","name":"Old Model","enabled":false,"status":"deprecated","capabilities":{"input":["text"]},"variants":[]}
		]}`))
	})
	mux.HandleFunc("/api/agent", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[
			{"id":"build","name":"Build","description":"implementation","mode":"primary","hidden":false},
			{"id":"explore","name":"Explore","mode":"subagent","hidden":false},
			{"id":"hidden","name":"Hidden","mode":"primary","hidden":true}
		]}`))
	})
	mux.HandleFunc("/api/session/ses_test", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"agent":"build","model":{"id":"model-a","providerID":"provider-a","variant":"high"}}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	run := &StructuredRun{
		id: "run", sessionID: "ses_test", baseURL: server.URL, password: "secret",
		client: server.Client(),
	}
	options, err := run.Options(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Models) != 1 || options.Models[0].ID != "model-a" {
		t.Fatalf("unexpected models: %+v", options.Models)
	}
	if len(options.Models[0].Variants) != 2 || options.Models[0].Inputs[1] != "image" {
		t.Fatalf("model capabilities not exposed: %+v", options.Models[0])
	}
	if len(options.Modes) != 1 || options.Modes[0].ID != "build" {
		t.Fatalf("subagent/hidden modes should be filtered: %+v", options.Modes)
	}
	if options.CurrentModel != "model-a" || options.CurrentVariant != "high" || options.CurrentMode != "build" {
		t.Fatalf("current settings not exposed: %+v", options)
	}
}

func TestWritePromptAttachments(t *testing.T) {
	runID := "test-attachments"
	t.Cleanup(func() { cleanupPromptAttachments(runID) })
	data := base64.StdEncoding.EncodeToString([]byte("hello"))
	files, err := writePromptAttachments(runID, []protocol.Attachment{{Name: "../x.txt", MIME: "text/plain", Data: data}})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected one file, got %d", len(files))
	}
	if files[0].Name != "x.txt" {
		t.Fatalf("attachment name was not sanitized: %q", files[0].Name)
	}
	content, err := os.ReadFile(files[0].Path)
	if err != nil || string(content) != "hello" {
		t.Fatalf("attachment content mismatch: %q %v", content, err)
	}
	if _, err := writePromptAttachments(runID, []protocol.Attachment{{Name: "bad", Data: "!"}}); err == nil {
		t.Fatal("invalid base64 was accepted")
	}
}

func TestStructuredPromptKeepsNativeSessionAndAppliesOptions(t *testing.T) {
	var modelBody map[string]any
	var agentBody map[string]any
	var promptBody map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/api/session/ses_native/model", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&modelBody)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/session/ses_native/agent", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&agentBody)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/api/session/ses_native/prompt", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&promptBody)
		_, _ = w.Write([]byte(`{}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	run := &StructuredRun{
		id: "run-prompt", sessionID: "ses_native", baseURL: server.URL,
		password: "secret", client: server.Client(),
		daemon: &Daemon{log: discardLogger()},
	}
	t.Cleanup(func() { cleanupPromptAttachments(run.id) })
	err := run.Prompt("hello", &protocol.PromptOptions{
		Model: "model-a", Provider: "provider-a", Variant: "high", AgentMode: "build",
		Attachments: []protocol.Attachment{{Name: "notes.txt", MIME: "text/plain", Data: base64.StdEncoding.EncodeToString([]byte("hello file"))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	model, _ := modelBody["model"].(map[string]any)
	if model["id"] != "model-a" || model["providerID"] != "provider-a" || model["variant"] != "high" {
		t.Fatalf("model switch payload: %+v", modelBody)
	}
	if agentBody["agent"] != "build" {
		t.Fatalf("agent switch payload: %+v", agentBody)
	}
	if promptBody["text"] != "hello" {
		t.Fatalf("prompt text missing: %+v", promptBody)
	}
	files, _ := promptBody["files"].([]any)
	if len(files) != 1 {
		t.Fatalf("attachment missing from prompt: %+v", promptBody)
	}
	file := files[0].(map[string]any)
	uri, _ := file["uri"].(string)
	if !strings.HasPrefix(uri, "file://") || !strings.Contains(uri, "notes.txt") {
		t.Fatalf("unexpected attachment URI: %q", uri)
	}
}
