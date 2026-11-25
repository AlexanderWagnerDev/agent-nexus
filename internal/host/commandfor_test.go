package host

import "testing"

func TestCmdUnsafe(t *testing.T) {
	safe := []string{
		"C:\\Program Files (x86)\\nodejs\\npm.cmd",
		"claude.cmd",
		"--resume",
		"3f2a1b4c-5d6e-7f80-9a1b-2c3d4e5f6a7b",
		"127.0.0.1",
		"--hostname",
		"C:\\Users\\dev\\project",
	}
	for _, arg := range safe {
		if bad, ok := cmdUnsafe([]string{arg}); ok {
			t.Errorf("cmdUnsafe(%q) = %q, true; want false", arg, bad)
		}
	}
	unsafe := []string{"a&calc", "a|b", "a>b", "a<b", "a^b", `a"b`, "%PATH%", "a\rb", "a\nb"}
	for _, arg := range unsafe {
		if bad, ok := cmdUnsafe([]string{"--flag", arg}); !ok || bad != arg {
			t.Errorf("cmdUnsafe(%q) = %q, %v; want %q, true", arg, bad, ok, arg)
		}
	}
}
