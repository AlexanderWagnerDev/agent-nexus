package host

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestChoosePermissionOption(t *testing.T) {
	options := []acp.PermissionOption{
		{OptionId: "yes", Kind: acp.PermissionOptionKindAllowOnce},
		{OptionId: "no", Kind: acp.PermissionOptionKindRejectOnce},
	}
	if got := choosePermissionOption(options, true); got == nil || got.OptionId != "yes" {
		t.Fatalf("allow picked %+v", got)
	}
	if got := choosePermissionOption(options, false); got == nil || got.OptionId != "no" {
		t.Fatalf("reject picked %+v", got)
	}
	if got := choosePermissionOption(nil, true); got != nil {
		t.Fatalf("empty options picked %+v", got)
	}
}

func TestACPCommandOverride(t *testing.T) {
	cfg := Config{ACPCommands: map[string]string{"custom": "my-acp-server --stdio"}}
	if got := cfg.ACPCommand("custom"); got != "my-acp-server --stdio" {
		t.Fatalf("override = %q", got)
	}
	if got := cfg.ACPCommand("missing"); got != "" {
		t.Fatalf("missing agent = %q", got)
	}
}
