package hub

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestPromptTemplatesOwnershipAndSharing(t *testing.T) {
	hub := newAPITestHub(t, Config{})
	hub.setupOK()
	owner := hub.testOwner()
	status, body := hub.do(nil, http.MethodPost, "/api/v1/templates", map[string]any{
		"name": "Review", "body": "Review {{branch}} in {{project}}", "shared": true,
	}, nil)
	expectStatus(t, status, http.StatusCreated, body, "create shared template")
	var created PromptTemplate
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatal(err)
	}
	if !created.Shared || !created.Owner || created.ID == "" {
		t.Fatalf("created template: %+v", created)
	}
	status, body = hub.do(nil, http.MethodGet, "/api/v1/templates", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "list templates")
	var list struct {
		Templates []PromptTemplate `json:"templates"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Templates) != 1 || list.Templates[0].Name != "Review" {
		t.Fatalf("templates: %+v", list.Templates)
	}

	member, token := hub.sessionFor("member", "template-member")
	_ = member
	status, body = hub.do(hub.clientWithToken(token), http.MethodPost, "/api/v1/templates", map[string]any{
		"name": "Shared by member", "body": "nope", "shared": true,
	}, nil)
	expectStatus(t, status, http.StatusForbidden, body, "member shared template")
	status, body = hub.do(hub.clientWithToken(token), http.MethodPost, "/api/v1/templates", map[string]any{
		"name": "Private", "body": "hello",
	}, nil)
	expectStatus(t, status, http.StatusCreated, body, "member private template")
	var private PromptTemplate
	if err := json.Unmarshal(body, &private); err != nil {
		t.Fatal(err)
	}

	// The member sees the admin's shared template plus their own.
	status, body = hub.do(hub.clientWithToken(token), http.MethodGet, "/api/v1/templates", nil, nil)
	expectStatus(t, status, http.StatusOK, body, "member template list")
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Templates) != 2 {
		t.Fatalf("member templates: %+v", list.Templates)
	}
	// A member cannot edit or delete the admin's template.
	status, body = hub.do(hub.clientWithToken(token), http.MethodPut, "/api/v1/templates/"+created.ID,
		map[string]any{"name": "Hijacked", "body": "no"}, nil)
	expectStatus(t, status, http.StatusNotFound, body, "member edit shared")
	status, body = hub.do(hub.clientWithToken(token), http.MethodDelete, "/api/v1/templates/"+created.ID, nil, nil)
	expectStatus(t, status, http.StatusNotFound, body, "member delete shared")
	status, body = hub.do(hub.clientWithToken(token), http.MethodPut, "/api/v1/templates/"+private.ID,
		map[string]any{"name": "Private edited", "body": "edited"}, nil)
	expectStatus(t, status, http.StatusOK, body, "member edit own")
	_ = owner
}
