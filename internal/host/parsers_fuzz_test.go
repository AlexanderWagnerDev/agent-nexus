package host

import (
	"encoding/json"
	"testing"
)

func FuzzClaudeContent(f *testing.F) {
	f.Add([]byte(`[{"type":"text","text":"hello"}]`))
	f.Add([]byte(`"plain"`))
	f.Add([]byte(`{bad`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		texts, _, tools, results := claudeContent(json.RawMessage(raw))
		_ = texts
		_ = tools
		_ = results
	})
}

func FuzzCodexContent(f *testing.F) {
	f.Add([]byte(`[{"type":"input_text","text":"hello"}]`))
	f.Add([]byte(`"plain"`))
	f.Add([]byte(`{bad`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_ = codexContent(json.RawMessage(raw))
	})
}

func FuzzGeminiText(f *testing.F) {
	f.Add("plain text")
	f.Add("")
	f.Fuzz(func(t *testing.T, text string) {
		_ = geminiText(text)
		_ = cleanTitle(text)
		_ = parseTime(text)
		_ = truncate(text, 20)
	})
}

func FuzzOpenCodeSnippet(f *testing.F) {
	f.Add(`{"text":"retry the upload"}`, "retry")
	f.Add(`{"content":[{"type":"text","text":"cap"}]}`, "cap")
	f.Add(`{bad`, "x")
	f.Fuzz(func(t *testing.T, data, query string) {
		_ = v2Snippet(data, query)
	})
}

func FuzzCodexRequestID(f *testing.F) {
	f.Add("42")
	f.Add("-1")
	f.Add("not-a-number")
	f.Fuzz(func(t *testing.T, id string) {
		_, _ = parseRequestID(id)
	})
}

func FuzzDevinText(f *testing.F) {
	f.Add([]byte(`"plain"`))
	f.Add([]byte(`[{"type":"text","text":"one"}]`))
	f.Add([]byte(`{"content":"three"}`))
	f.Add([]byte(`{bad`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		_ = devinText(json.RawMessage(raw))
		_ = devinToolCalls(json.RawMessage(raw))
	})
}

func FuzzCursorUserText(f *testing.F) {
	f.Add("<timestamp>Monday</timestamp>\n<user_query>hello</user_query>")
	f.Add("plain prompt")
	f.Add("")
	f.Fuzz(func(t *testing.T, text string) {
		_ = cursorUserText(text)
	})
}
