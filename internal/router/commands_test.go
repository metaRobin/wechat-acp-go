package router

import "testing"

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
		name    string
		arg     string
	}{
		{"/use claude", false, "use", "claude"},
		{"/use   gemini  ", false, "use", "gemini"},
		{"/use", false, "use", ""},
		{"/agents", false, "agents", ""},
		{"/status", false, "status", ""},
		{"/STATUS", false, "status", ""},
		{"/clear", false, "clear", ""},
		{"/CLEAR", false, "clear", ""},
		{"/Use Claude", false, "use", "Claude"},

		// Not commands — should return nil
		{"hello", true, "", ""},
		{"/help me with code", true, "", ""},
		{"/foo bar", true, "", ""},
		{"use claude", true, "", ""},
		{"", true, "", ""},
		{" /use claude", false, "use", "claude"}, // trimmed
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cmd := ParseCommand(tt.input)
			if tt.wantNil {
				if cmd != nil {
					t.Errorf("ParseCommand(%q) = %+v, want nil", tt.input, cmd)
				}
				return
			}
			if cmd == nil {
				t.Fatalf("ParseCommand(%q) = nil, want command", tt.input)
			}
			if cmd.Name != tt.name {
				t.Errorf("Name = %q, want %q", cmd.Name, tt.name)
			}
			if cmd.Arg != tt.arg {
				t.Errorf("Arg = %q, want %q", cmd.Arg, tt.arg)
			}
		})
	}
}
