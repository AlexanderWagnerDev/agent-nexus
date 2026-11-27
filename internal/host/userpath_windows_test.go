//go:build windows

package host

import "testing"

func TestExpandRegistryPath(t *testing.T) {
	t.Setenv("AGENTNEXUS_TEST_DIR", `C:\tools`)
	if got, want := expandRegistryPath(`%AGENTNEXUS_TEST_DIR%\bin`), `C:\tools\bin`; got != want {
		t.Fatalf("expandRegistryPath = %q, want %q", got, want)
	}
	const unset = `%AGENTNEXUS_NOT_SET_91f2%\bin`
	if got := expandRegistryPath(unset); got != unset {
		t.Fatalf("unknown variable was not kept literal: %q", got)
	}
}

func TestMergeUserPath(t *testing.T) {
	const dir = `C:\Users\me\AppData\Local\AgentNexus`
	for name, tc := range map[string]struct {
		current string
		want    string
		changed bool
	}{
		"empty":              {current: "", want: dir, changed: true},
		"only semicolons":    {current: ";;", want: dir, changed: true},
		"missing":            {current: `C:\Windows`, want: `C:\Windows;` + dir, changed: true},
		"present":            {current: `C:\Windows;` + dir, want: `C:\Windows;` + dir, changed: false},
		"case and slash":     {current: `C:\Windows;c:\users\me\appdata\local\agentnexus\`, want: `C:\Windows;c:\users\me\appdata\local\agentnexus\`, changed: false},
		"expanded untouched": {current: `%USERPROFILE%\bin;` + dir, want: `%USERPROFILE%\bin;` + dir, changed: false},
	} {
		got, changed := mergeUserPath(tc.current, dir)
		if got != tc.want || changed != tc.changed {
			t.Fatalf("%s: mergeUserPath(%q) = %q, %v; want %q, %v", name, tc.current, got, changed, tc.want, tc.changed)
		}
	}
}
