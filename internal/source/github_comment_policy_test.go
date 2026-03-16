package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEvaluateGitHubCommentPolicy_IgnoresUnauthorizedLatestExclude(t *testing.T) {
	policy := githubCommentPolicy{
		TriggerComment:  "/kelos pick-up",
		ExcludeComments: []string{"/kelos needs-input"},
		AllowedUsers:    []string{"alice"},
	}

	authorizer, err := newGitHubCommentAuthorizer("owner", "repo", "", "", nil, policy)
	if err != nil {
		t.Fatalf("newGitHubCommentAuthorizer() error = %v", err)
	}

	allowed, triggerTime, err := evaluateGitHubCommentPolicy(
		context.Background(),
		"",
		githubUser{},
		[]githubComment{
			{
				Body:      "/kelos pick-up",
				CreatedAt: "2026-01-02T12:00:00Z",
				User:      githubUser{Login: "alice"},
			},
			{
				Body:      "/kelos needs-input",
				CreatedAt: "2026-01-03T12:00:00Z",
				User:      githubUser{Login: "mallory"},
			},
		},
		policy,
		authorizer,
	)
	if err != nil {
		t.Fatalf("evaluateGitHubCommentPolicy() error = %v", err)
	}
	if !allowed {
		t.Fatal("Expected authorized trigger to win after unauthorized exclude was ignored")
	}

	wantTriggerTime := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	if !triggerTime.Equal(wantTriggerTime) {
		t.Fatalf("TriggerTime = %v, want %v", triggerTime, wantTriggerTime)
	}
}

func TestEvaluateGitHubCommentPolicy_BodyRequiresAuthorizedActor(t *testing.T) {
	policy := githubCommentPolicy{
		TriggerComment: "/kelos pick-up",
		AllowedUsers:   []string{"alice"},
	}

	authorizer, err := newGitHubCommentAuthorizer("owner", "repo", "", "", nil, policy)
	if err != nil {
		t.Fatalf("newGitHubCommentAuthorizer() error = %v", err)
	}

	tests := []struct {
		name      string
		bodyActor githubUser
		want      bool
	}{
		{
			name:      "authorized actor",
			bodyActor: githubUser{Login: "alice"},
			want:      true,
		},
		{
			name:      "unauthorized actor",
			bodyActor: githubUser{Login: "mallory"},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed, _, err := evaluateGitHubCommentPolicy(
				context.Background(),
				"/kelos pick-up",
				tt.bodyActor,
				nil,
				policy,
				authorizer,
			)
			if err != nil {
				t.Fatalf("evaluateGitHubCommentPolicy() error = %v", err)
			}
			if allowed != tt.want {
				t.Fatalf("Allowed = %v, want %v", allowed, tt.want)
			}
		})
	}
}

func TestGitHubCommentAuthorizer_MinimumPermission(t *testing.T) {
	var permissionChecks int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/collaborators/alice/permission":
			permissionChecks++
			json.NewEncoder(w).Encode(githubPermissionResponse{Permission: "read"})
		case "/repos/owner/repo/collaborators/bob/permission":
			permissionChecks++
			json.NewEncoder(w).Encode(githubPermissionResponse{Permission: "admin"})
		case "/repos/owner/repo/collaborators/carol/permission":
			permissionChecks++
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	authorizer, err := newGitHubCommentAuthorizer(
		"owner",
		"repo",
		server.URL,
		"",
		server.Client(),
		githubCommentPolicy{MinimumPermission: "write"},
	)
	if err != nil {
		t.Fatalf("newGitHubCommentAuthorizer() error = %v", err)
	}

	tests := []struct {
		login string
		want  bool
	}{
		{login: "alice", want: false},
		{login: "bob", want: true},
		{login: "carol", want: false},
	}

	for _, tt := range tests {
		got, err := authorizer.isAuthorizedLogin(context.Background(), tt.login)
		if err != nil {
			t.Fatalf("isAuthorizedLogin(%q) error = %v", tt.login, err)
		}
		if got != tt.want {
			t.Fatalf("isAuthorizedLogin(%q) = %v, want %v", tt.login, got, tt.want)
		}
	}

	// The second lookup should hit the cache.
	if _, err := authorizer.isAuthorizedLogin(context.Background(), "bob"); err != nil {
		t.Fatalf("cached isAuthorizedLogin() error = %v", err)
	}
	if permissionChecks != 3 {
		t.Fatalf("permission checks = %d, want %d", permissionChecks, 3)
	}
}

func TestGitHubCommentAuthorizer_AllowedTeams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/my-org/teams/platform/memberships/alice":
			json.NewEncoder(w).Encode(githubMembershipResponse{State: "active"})
		case "/orgs/my-org/teams/platform/memberships/mallory":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	authorizer, err := newGitHubCommentAuthorizer(
		"owner",
		"repo",
		server.URL,
		"",
		server.Client(),
		githubCommentPolicy{AllowedTeams: []string{"my-org/platform"}},
	)
	if err != nil {
		t.Fatalf("newGitHubCommentAuthorizer() error = %v", err)
	}

	if got, err := authorizer.isAuthorizedLogin(context.Background(), "alice"); err != nil || !got {
		t.Fatalf("isAuthorizedLogin(alice) = %v, %v, want true, nil", got, err)
	}
	if got, err := authorizer.isAuthorizedLogin(context.Background(), "mallory"); err != nil || got {
		t.Fatalf("isAuthorizedLogin(mallory) = %v, %v, want false, nil", got, err)
	}
}

func TestGitHubSourceDiscover_ReturnsAuthorizationLookupError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/issues":
			json.NewEncoder(w).Encode([]githubIssue{
				{
					Number:  1,
					Title:   "Issue",
					HTMLURL: "https://github.com/owner/repo/issues/1",
				},
			})
		case "/repos/owner/repo/issues/1/comments":
			json.NewEncoder(w).Encode([]githubComment{
				{
					Body:      "/kelos pick-up",
					CreatedAt: "2026-01-02T12:00:00Z",
					User:      githubUser{Login: "alice"},
				},
			})
		case "/repos/owner/repo/collaborators/alice/permission":
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"boom"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	s := &GitHubSource{
		Owner:             "owner",
		Repo:              "repo",
		BaseURL:           server.URL,
		Client:            server.Client(),
		TriggerComment:    "/kelos pick-up",
		MinimumPermission: "write",
	}

	if _, err := s.Discover(context.Background()); err == nil {
		t.Fatal("Expected Discover() to fail when authorization lookup returns 500")
	}
}

func TestDiscoverPullRequests_CommentPolicyIgnoresUnauthorizedReviewBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/pulls":
			json.NewEncoder(w).Encode([]githubPullRequest{
				{
					Number:  1,
					Title:   "Fix flaky test",
					HTMLURL: "https://github.com/owner/repo/pull/1",
					Head: githubPullRequestHead{
						Ref: "kelos-task-123",
						SHA: "head-sha-1",
					},
				},
			})
		case "/repos/owner/repo/pulls/1/reviews":
			json.NewEncoder(w).Encode([]githubPullRequestReview{
				{
					Body:        "/kelos needs-input",
					State:       "COMMENTED",
					SubmittedAt: "2026-01-03T12:00:00Z",
					CommitID:    "head-sha-1",
					User:        githubUser{Login: "mallory"},
				},
			})
		case "/repos/owner/repo/issues/1/comments":
			json.NewEncoder(w).Encode([]githubComment{
				{
					Body:      "/kelos pick-up",
					CreatedAt: "2026-01-02T12:00:00Z",
					User:      githubUser{Login: "alice"},
				},
			})
		case "/repos/owner/repo/pulls/1/comments":
			json.NewEncoder(w).Encode([]githubPullRequestComment{})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	s := &GitHubPullRequestSource{
		Owner:           "owner",
		Repo:            "repo",
		BaseURL:         server.URL,
		Client:          server.Client(),
		TriggerComment:  "/kelos pick-up",
		ExcludeComments: []string{"/kelos needs-input"},
		AllowedUsers:    []string{"alice"},
	}

	items, err := s.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Number != 1 {
		t.Fatalf("Number = %d, want %d", items[0].Number, 1)
	}
}
