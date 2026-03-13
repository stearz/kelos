package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

const defaultBaseURL = "https://api.github.com"

// GitHubReporter posts and updates issue/PR comments on GitHub.
// TokenFile is the path to a file containing the GitHub token. When set,
// the token is re-read from disk on every API call so that rotated
// credentials (e.g. GitHub App installation tokens refreshed by a sidecar)
// are picked up automatically.
type GitHubReporter struct {
	Owner     string
	Repo      string
	Token     string // static token (used when TokenFile is empty)
	TokenFile string // path to token file; re-read on each request
	BaseURL   string
	Client    *http.Client
}

func (r *GitHubReporter) baseURL() string {
	if r.BaseURL != "" {
		return r.BaseURL
	}
	return defaultBaseURL
}

func (r *GitHubReporter) httpClient() *http.Client {
	if r.Client != nil {
		return r.Client
	}
	return http.DefaultClient
}

type createCommentRequest struct {
	Body string `json:"body"`
}

type commentResponse struct {
	ID int64 `json:"id"`
}

// CreateComment creates a comment on a GitHub issue or pull request and returns
// the comment ID.
func (r *GitHubReporter) CreateComment(ctx context.Context, number int, body string) (int64, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/%d/comments", r.baseURL(), r.Owner, r.Repo, number)

	payload, err := json.Marshal(createCommentRequest{Body: body})
	if err != nil {
		return 0, fmt.Errorf("marshalling comment body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}
	r.setHeaders(req)

	resp, err := r.httpClient().Do(req)
	if err != nil {
		return 0, fmt.Errorf("posting comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return 0, fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	var result commentResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decoding comment response: %w", err)
	}

	return result.ID, nil
}

// UpdateComment updates an existing GitHub comment by its ID.
func (r *GitHubReporter) UpdateComment(ctx context.Context, commentID int64, body string) error {
	url := fmt.Sprintf("%s/repos/%s/%s/issues/comments/%s", r.baseURL(), r.Owner, r.Repo, strconv.FormatInt(commentID, 10))

	payload, err := json.Marshal(createCommentRequest{Body: body})
	if err != nil {
		return fmt.Errorf("marshalling comment body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	r.setHeaders(req)

	resp, err := r.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("updating comment: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("GitHub API returned status %d: %s", resp.StatusCode, string(errBody))
	}

	return nil
}

// resolveToken returns the current GitHub token. When TokenFile is set the
// token is re-read from disk on every call so that rotated credentials are
// picked up automatically. Falls back to the static Token field.
func (r *GitHubReporter) resolveToken() string {
	if r.TokenFile != "" {
		data, err := os.ReadFile(r.TokenFile)
		if err == nil {
			if t := strings.TrimSpace(string(data)); t != "" {
				return t
			}
		}
	}
	return r.Token
}

func (r *GitHubReporter) setHeaders(req *http.Request) {
	if token := r.resolveToken(); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")
}

// FormatAcceptedComment returns the comment body for an accepted task.
func FormatAcceptedComment(taskName string) string {
	return fmt.Sprintf("🤖 **Kelos Task Status**\n\nTask `%s` has been **accepted** and is being processed.", taskName)
}

// FormatSucceededComment returns the comment body for a succeeded task.
func FormatSucceededComment(taskName string) string {
	return fmt.Sprintf("🤖 **Kelos Task Status**\n\nTask `%s` has **succeeded**. ✅", taskName)
}

// FormatFailedComment returns the comment body for a failed task.
func FormatFailedComment(taskName string) string {
	return fmt.Sprintf("🤖 **Kelos Task Status**\n\nTask `%s` has **failed**. ❌", taskName)
}
