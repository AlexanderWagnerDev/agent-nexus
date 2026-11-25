//go:build windows

package host

import "testing"

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
