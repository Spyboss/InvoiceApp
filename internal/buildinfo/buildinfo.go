// Package buildinfo reports what is actually running, so a deploy can be
// identified from /healthz and the logs without guessing.
package buildinfo

import (
	"os"
	"strings"
)

// Version is set at build time with
//
//	-ldflags "-X github.com/spyboss/invoiceapp/internal/buildinfo.Version=1.2.3"
var Version = "dev"

// CommitSHA is set at build time the same way. When it is empty, the commit
// Railway injects into the runtime environment is used instead.
var CommitSHA = ""

// Commit is the short commit the process was built from.
func Commit() string {
	if CommitSHA != "" {
		return shorten(CommitSHA)
	}
	if sha := os.Getenv("RAILWAY_GIT_COMMIT_SHA"); sha != "" {
		return shorten(sha)
	}
	return "local"
}

func shorten(sha string) string {
	sha = strings.TrimSpace(sha)
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
