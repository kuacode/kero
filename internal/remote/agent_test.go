package remote

import "testing"

func TestAgentLine(t *testing.T) {
	for _, c := range []struct{ out, want string }{
		{"\n" + agentMark + "/tmp/agent.sock\n", "/tmp/agent.sock"},
		{"Welcome!\n\n" + agentMark + "/Users/me/Library/Group Containers/2BUI8C4Y56.com.1password/t/agent.sock\n", "/Users/me/Library/Group Containers/2BUI8C4Y56.com.1password/t/agent.sock"},
		{"\n" + agentMark + "\n", ""},
		{agentMark + "/not/after/a/newline\n", ""},
		{"", ""},
	} {
		if got := agentLine([]byte(c.out)); got != c.want {
			t.Errorf("agentLine(%q) = %q, want %q", c.out, got, c.want)
		}
	}
}

// DistroID reads /etc/os-release for the Linux distribution. On this
// machine (Ubuntu), it names ubuntu; elsewhere the test only checks it
// answers without an error.
func TestDistroID(t *testing.T) {
	d := DistroID()
	if d == "" {
		t.Skip("no /etc/os-release (not a Linux system)")
	}
}
