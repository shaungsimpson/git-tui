package gitx

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Viewer is the person using the app: the GitHub account gh is logged in to, and the local git identity.
type Viewer struct {
	Login, Name, Bio, Company, Location, AvatarURL string
	Followers, Following                           int
	GitName, GitEmail                              string
	GitHubErr                                      string // why the GitHub account is unknown; the git identity still shows
}

// Viewer reads the git identity in dir (so per-directory includes apply) and, at the same time, the gh account.
// Only when neither is known is it an error.
func (g Git) Viewer(ctx context.Context, dir string) (Viewer, error) {
	gh := background(func() (string, error) { return command(ctx, dir, ghTimeout, "gh", "api", "user") })
	name, _ := g.run(ctx, dir, "config", "user.name") // unset is not an error worth showing
	email, _ := g.run(ctx, dir, "config", "user.email")
	var v Viewer
	out, err := gh()
	if err == nil {
		v, err = ParseViewer(out)
	}
	v.GitName, v.GitEmail = strings.TrimSpace(name), strings.TrimSpace(email)
	if err != nil {
		if v.GitName == "" && v.GitEmail == "" {
			return v, err
		}
		v.GitHubErr = err.Error()
	}
	return v, nil
}

// ParseViewer parses `gh api user` output.
func ParseViewer(out string) (Viewer, error) {
	var raw struct {
		Login, Name, Bio, Company, Location string
		AvatarURL                           string `json:"avatar_url"`
		Followers, Following                int
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		return Viewer{}, fmt.Errorf("parse gh output: %w", err)
	}
	if raw.Login == "" {
		return Viewer{}, fmt.Errorf("gh api user: no login in the response")
	}
	return Viewer{
		Login: raw.Login, Name: raw.Name, Bio: raw.Bio, Company: raw.Company, Location: raw.Location,
		AvatarURL: raw.AvatarURL, Followers: raw.Followers, Following: raw.Following,
	}, nil
}
