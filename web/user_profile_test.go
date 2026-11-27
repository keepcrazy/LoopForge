package web

import (
	"os/exec"
	"testing"
)

func TestUserProfileHelpers(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	if out, err := exec.Command(node, "user_profile_test.js", appBundlePath(t)).CombinedOutput(); err != nil {
		t.Fatalf("user profile helpers: %v\n%s", err, out)
	}
}
