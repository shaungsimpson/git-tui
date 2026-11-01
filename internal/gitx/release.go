package gitx

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/shaungsimpson/git-tui/internal/update"
)

// LatestRelease reads git-tui's newest published release; GitHub leaves drafts and prereleases out of it.
func (Git) LatestRelease(ctx context.Context) (update.Latest, error) {
	out, err := command(ctx, "", ghTimeout, "gh", "api", "repos/"+update.Repo+"/releases/latest")
	if err != nil {
		return update.Latest{}, err
	}
	return ParseRelease(out)
}

// ParseRelease parses `gh api repos/…/releases/latest` and accepts only a stable version.
func ParseRelease(out string) (update.Latest, error) {
	var raw struct {
		Tag               string `json:"tag_name"`
		Draft, Prerelease bool
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return update.Latest{}, fmt.Errorf("parse gh output: %w", err)
	}
	if raw.Draft || raw.Prerelease || !update.Stable(raw.Tag) {
		return update.Latest{}, fmt.Errorf("latest release %q is not a stable version", raw.Tag)
	}
	return update.Latest{Version: raw.Tag}, nil
}
