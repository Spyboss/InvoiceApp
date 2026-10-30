package buildinfo

import "testing"

func TestCommit(t *testing.T) {
	t.Run("build time sha wins", func(t *testing.T) {
		CommitSHA = "0123456789abcdef"
		t.Cleanup(func() { CommitSHA = "" })
		if got := Commit(); got != "0123456" {
			t.Errorf("Commit() = %q, want 0123456", got)
		}
	})

	t.Run("railway runtime sha is the fallback", func(t *testing.T) {
		t.Setenv("RAILWAY_GIT_COMMIT_SHA", "fedcba9876543210")
		if got := Commit(); got != "fedcba9" {
			t.Errorf("Commit() = %q, want fedcba9", got)
		}
	})

	t.Run("local when nothing is known", func(t *testing.T) {
		t.Setenv("RAILWAY_GIT_COMMIT_SHA", "")
		if got := Commit(); got != "local" {
			t.Errorf("Commit() = %q, want local", got)
		}
	})
}
