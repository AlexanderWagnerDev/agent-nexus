package hub

import (
	"testing"

	"github.com/AlexanderWagnerDev/agent-nexus/internal/protocol"
)

func TestClassifyRunError(t *testing.T) {
	cases := map[string]string{
		"dial tcp 127.0.0.1: connection refused":         ErrCodeNetwork,
		"request timed out":                              ErrCodeTimeout,
		"401 unauthorized":                               ErrCodeAuthentication,
		"403 permission required":                        ErrCodeAuthorization,
		"session is already active":                      ErrCodeSessionConflict,
		"working directory is outside the allowed roots": ErrCodeHostPolicy,
		"agent version is outside the tested range":      ErrCodeUnsupported,
		"opencode executable not found":                  ErrCodeAgentUnavailable,
		"invalid host response":                          ErrCodeAgentProtocol,
		"process exited with exit status 2":              ErrCodeProcessCrash,
		"sqlite database is locked":                      ErrCodeDatabase,
		"update archive checksum mismatch":               ErrCodeUpdate,
		"something unexpected":                           ErrCodeInternal,
	}
	for message, want := range cases {
		if got := ClassifyRunError(message); got != want {
			t.Errorf("ClassifyRunError(%q) = %q, want %q", message, got, want)
		}
	}
}

func TestSetRunErrorPersistsClassification(t *testing.T) {
	hub := newSetupHub(t)
	owner, _ := hub.store.UserByUsername("owner")
	hostID, err := hub.store.CreateHost(owner.ID, "error-host", make([]byte, 32), "linux", "amd64", "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.store.CreateRun("run-error", "", hostID, protocol.ModeStructured, owner.ID, "d"); err != nil {
		t.Fatal(err)
	}
	hub.store.SetRunError("run-error", ClassifyRunError("session is already active"), "session is already active")
	run, _, err := hub.store.RunByID("run-error")
	if err != nil || run.ErrorCode != ErrCodeSessionConflict || run.ErrorMessage == "" {
		t.Fatalf("run error: %+v %v", run, err)
	}
}
