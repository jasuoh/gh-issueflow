package flow

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasuoh/gh-issueflow/internal/git"
	"github.com/jasuoh/gh-issueflow/internal/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var boardStatuses = []string{"Todo", "In Progress", "Needs review", "Done"}

const projectNumber = 7

type fixture struct {
	t    *testing.T
	app  *App
	mock *GitHubMock
	dir  string
	out  *bytes.Buffer
}

// setup creates a repository whose origin looks like a github.com
// repository, while git transparently talks to a local bare repository, and
// an App that talks to the mock instead of GitHub.
func setup(t *testing.T, mock *GitHubMock, withProject bool) *fixture {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")

	root := t.TempDir()
	dir := filepath.Join(root, "repo")
	origin := filepath.Join(root, "origin.git")
	githubURL := fmt.Sprintf("https://github.com/%s/%s.git", GitHubMockOwner, GitHubMockRepo)

	f := &fixture{t: t, mock: mock, dir: dir, out: &bytes.Buffer{}}
	f.git(root, "init", "-q", "-b", "master", dir)
	f.commit("initial commit")
	f.git(root, "clone", "-q", "--bare", dir, origin)
	f.sh("remote", "add", "origin", githubURL)
	f.sh("config", "url."+origin+".insteadOf", githubURL)
	f.sh("fetch", "-q", "origin")
	f.sh("branch", "--set-upstream-to=origin/master", "master")

	mock.remotePath = origin
	server := httptest.NewServer(http.HandlerFunc(mock.serveHTTP))
	t.Cleanup(server.Close)

	gitRepo := &git.Repo{Dir: dir}
	config, err := LoadConfig(gitRepo)
	require.NoError(t, err)
	if withProject {
		config.ProjectNumber = projectNumber
	}

	f.app = &App{
		Git:    gitRepo,
		Client: github.NewClient(server.URL, server.Client(), GitHubMockOwner, GitHubMockRepo),
		Host:   "github.com",
		Remote: "origin",
		Config: config,
		Cache:  &ProjectCache{Path: filepath.Join(root, "cache", "projects.json")},
		Out:    f.out,
	}
	return f
}

func (self *fixture) git(dir string, args ...string) string {
	self.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(self.t, err, "git %s: %s", strings.Join(args, " "), output)
	return strings.TrimSpace(string(output))
}

// sh runs git in the test repository.
func (self *fixture) sh(args ...string) string {
	self.t.Helper()
	return self.git(self.dir, args...)
}

func (self *fixture) commit(message string) {
	self.t.Helper()
	self.sh("commit", "-q", "--allow-empty", "-m", message)
}

func (self *fixture) currentBranch() string {
	return self.sh("symbolic-ref", "--short", "HEAD")
}

func (self *fixture) localBranches() []string {
	return strings.Split(self.sh("for-each-ref", "--format=%(refname:short)", "refs/heads"), "\n")
}

func (self *fixture) remoteHasBranch(branch string) bool {
	_, err := self.mock.remoteRevParse("refs/heads/" + branch)
	return err == nil
}

func (self *fixture) issue(number int) GitHubMockIssue {
	self.t.Helper()
	for _, issue := range self.mock.Issues {
		if issue.Number == number {
			return *issue
		}
	}
	self.t.Fatalf("no issue #%d", number)
	return GitHubMockIssue{}
}

func (self *fixture) pullRequest(number int) GitHubMockPullRequest {
	self.t.Helper()
	for _, pr := range self.mock.PullRequests {
		if pr.Number == number {
			return *pr
		}
	}
	self.t.Fatalf("no pull request #%d", number)
	return GitHubMockPullRequest{}
}

func TestIssues(t *testing.T) {
	f := setup(t, &GitHubMock{Issues: []*GitHubMockIssue{
		{Number: 3, Title: "Fix the login form"},
		{Number: 5, Title: "Add dark mode"},
	}}, false)

	require.NoError(t, f.app.Issues())

	assert.Equal(t, "5\tAdd dark mode\n3\tFix the login form\n", f.out.String())
}

func TestPick(t *testing.T) {
	f := setup(t, &GitHubMock{
		Issues:        []*GitHubMockIssue{{Number: 3, Title: "Fix the login form"}},
		StatusOptions: boardStatuses,
		ProjectNumber: projectNumber,
	}, true)

	require.NoError(t, f.app.Pick(3))

	assert.Equal(t, "3-fix-the-login-form", f.currentBranch())
	assert.Equal(t, "origin/3-fix-the-login-form", f.sh("rev-parse", "--abbrev-ref", "@{upstream}"))
	assert.Equal(t, []string{"test-user"}, f.issue(3).Assignees)
	assert.Equal(t, "In Progress", f.issue(3).Status)
	assert.Equal(t, "Picked #3: checked out 3-fix-the-login-form\n", f.out.String())
}

func TestPickWithoutProject(t *testing.T) {
	f := setup(t, &GitHubMock{
		Issues: []*GitHubMockIssue{{Number: 12, Title: "Crash on startup"}},
		// The token can't access projects, which mustn't matter as long as
		// no project is configured.
		MissingProjectScope: true,
	}, false)

	require.NoError(t, f.app.Pick(12))

	assert.Equal(t, "12-crash-on-startup", f.currentBranch())
	assert.Equal(t, []string{"test-user"}, f.issue(12).Assignees)
	assert.Equal(t, "", f.issue(12).Status)
}

func TestPickMissingProjectScope(t *testing.T) {
	f := setup(t, &GitHubMock{
		Issues:              []*GitHubMockIssue{{Number: 3, Title: "Fix the login form"}},
		StatusOptions:       boardStatuses,
		ProjectNumber:       projectNumber,
		MissingProjectScope: true,
	}, true)

	err := f.app.Pick(3)

	assert.ErrorIs(t, err, github.ErrInsufficientScopes)
	assert.Contains(t, TranslateError(err).Error(), "Run: gh auth refresh -s project")
	// Nothing was changed.
	assert.Equal(t, "master", f.currentBranch())
	assert.Empty(t, f.issue(3).Assignees)
}

func TestNew(t *testing.T) {
	f := setup(t, &GitHubMock{
		Issues:        []*GitHubMockIssue{{Number: 3, Title: "Fix the login form"}},
		PullRequests:  []*GitHubMockPullRequest{{Number: 4}},
		StatusOptions: boardStatuses,
		ProjectNumber: projectNumber,
	}, true)

	require.NoError(t, f.app.New("  Support dark mode "))

	issue := f.issue(5)
	assert.Equal(t, "Support dark mode", issue.Title)
	assert.Equal(t, []string{"test-user"}, issue.Assignees)
	assert.Equal(t, "(no status)", issue.Status)
	assert.Equal(t, "Created issue #5: https://github.com/test-owner/test-repo/issues/5\n", f.out.String())

	assert.EqualError(t, f.app.New(" "), "the issue title must not be empty")
}

func TestPullRequest(t *testing.T) {
	f := setup(t, &GitHubMock{
		Issues:        []*GitHubMockIssue{{Number: 3, Title: "Fix the login form", Status: "In Progress"}},
		StatusOptions: boardStatuses,
		ProjectNumber: projectNumber,
	}, true)
	f.sh("checkout", "-q", "-b", "3-fix-the-login-form")
	f.commit("Validate the login form")

	require.NoError(t, f.app.PullRequest())

	assert.Equal(t, GitHubMockPullRequest{
		Number:      4,
		HeadRefName: "3-fix-the-login-form",
		BaseRefName: "master",
		Title:       "Validate the login form",
		Body:        "Closes #3",
	}, f.pullRequest(4))
	assert.Equal(t, "Needs review", f.issue(3).Status)
	assert.Equal(t, "origin/3-fix-the-login-form", f.sh("rev-parse", "--abbrev-ref", "@{upstream}"))
	assert.Equal(t, "Created pull request #4:\nhttps://github.com/test-owner/test-repo/pull/4\n", f.out.String())
}

func TestPullRequestTitleForSeveralCommits(t *testing.T) {
	f := setup(t, &GitHubMock{Issues: []*GitHubMockIssue{{Number: 3, Title: "Fix the login form"}}}, false)
	f.sh("checkout", "-q", "-b", "3-fix-the-login-form")
	f.commit("Validate the email")
	f.commit("Validate the password")

	require.NoError(t, f.app.PullRequest())

	assert.Equal(t, "Fix the login form", f.pullRequest(4).Title)
}

func TestPullRequestWithoutCommits(t *testing.T) {
	f := setup(t, &GitHubMock{}, false)
	f.sh("checkout", "-q", "-b", "feature")

	assert.EqualError(t, f.app.PullRequest(), "the branch 'feature' has no commits that aren't on 'origin/master'")
}

func TestStatusesAndSetStatus(t *testing.T) {
	f := setup(t, &GitHubMock{
		Issues:        []*GitHubMockIssue{{Number: 3, Title: "Fix the login form", Status: "In Progress"}},
		StatusOptions: boardStatuses,
		ProjectNumber: projectNumber,
	}, true)
	f.sh("checkout", "-q", "-b", "3-fix-the-login-form")

	require.NoError(t, f.app.Statuses())
	assert.Equal(t, "Todo\nIn Progress\nNeeds review\nDone\n", f.out.String())

	f.out.Reset()
	require.NoError(t, f.app.SetStatus("done"))
	assert.Equal(t, "Done", f.issue(3).Status)
	assert.Equal(t, "Set status of #3 to 'done'\n", f.out.String())

	assert.EqualError(t, f.app.SetStatus("Blocked"), "the project field 'Status' has no option named 'Blocked'")
}

func TestStatusesWithoutProject(t *testing.T) {
	f := setup(t, &GitHubMock{}, false)

	assert.ErrorContains(t, f.app.Statuses(), "no project board configured")
}

func TestSetStatusOnBranchWithoutIssue(t *testing.T) {
	f := setup(t, &GitHubMock{StatusOptions: boardStatuses, ProjectNumber: projectNumber}, true)

	assert.EqualError(t, f.app.SetStatus("Done"), "the branch 'master' isn't named after an issue; its name must start with the issue number")
}

func setupMerge(t *testing.T, approvedBy []string) *fixture {
	f := setup(t, &GitHubMock{
		Issues: []*GitHubMockIssue{{Number: 3, Title: "Fix the login form", Status: "Needs review"}},
		PullRequests: []*GitHubMockPullRequest{
			{Number: 4, HeadRefName: "3-fix-the-login-form", BaseRefName: "master", ApprovedBy: approvedBy},
		},
		StatusOptions: boardStatuses,
		ProjectNumber: projectNumber,
	}, true)
	f.sh("checkout", "-q", "-b", "3-fix-the-login-form")
	f.commit("Validate the login form")
	f.sh("push", "-q", "-u", "origin", "3-fix-the-login-form")
	return f
}

func TestMerge(t *testing.T) {
	f := setupMerge(t, []string{"reviewer"})

	require.NoError(t, f.app.Merge())

	pr := f.pullRequest(4)
	assert.True(t, pr.Merged)
	assert.Equal(t, "SQUASH", pr.MergeMethod)
	assert.Equal(t, "Done", f.issue(3).Status)
	assert.Equal(t, "master", f.currentBranch())
	assert.Equal(t, []string{"master"}, f.localBranches())
	assert.False(t, f.remoteHasBranch("3-fix-the-login-form"))
	assert.Equal(t, "Merged pull request #4\n", f.out.String())
}

func TestMergeWithoutApprovalFromSomeoneElse(t *testing.T) {
	// Approving your own pull request doesn't count.
	f := setupMerge(t, []string{"test-user"})
	// As if GitHub deleted the head branch on merge.
	f.git(f.mock.remotePath, "branch", "-D", "3-fix-the-login-form")

	require.NoError(t, f.app.Merge())

	assert.True(t, f.pullRequest(4).Merged)
	assert.Equal(t, "Needs review", f.issue(3).Status)
	assert.Equal(t, []string{"master"}, f.localBranches())
	assert.Equal(t, "Merged pull request #4\nThe issue's status is unchanged, because nobody else approved the pull request.\n", f.out.String())
}

func TestMergeWithoutPullRequest(t *testing.T) {
	f := setup(t, &GitHubMock{}, false)
	f.sh("checkout", "-q", "-b", "feature")

	assert.EqualError(t, f.app.Merge(), "there is no open pull request for the branch 'feature'")
}

func TestBranchesAndDeleteBranch(t *testing.T) {
	f := setup(t, &GitHubMock{}, false)
	f.sh("checkout", "-q", "-b", "3-fix-the-login-form")
	f.commit("Validate the login form")
	f.sh("push", "-q", "-u", "origin", "3-fix-the-login-form")
	f.sh("checkout", "-q", "-b", "5-add-dark-mode", "master")
	f.commit("Add dark mode")
	f.sh("push", "-q", "-u", "origin", "5-add-dark-mode")
	// Deleted on GitHub, but the remote-tracking branch doesn't know yet.
	f.git(f.mock.remotePath, "branch", "-D", "5-add-dark-mode")
	f.sh("checkout", "-q", "master")

	require.NoError(t, f.app.Branches())
	assert.Equal(t, "3-fix-the-login-form\n5-add-dark-mode\n", f.out.String())

	require.NoError(t, f.app.DeleteBranch("3-fix-the-login-form"))
	require.NoError(t, f.app.DeleteBranch("5-add-dark-mode"))

	assert.Equal(t, []string{"master"}, f.localBranches())
	assert.False(t, f.remoteHasBranch("3-fix-the-login-form"))
	remoteTrackingBranches := f.sh("for-each-ref", "--format=%(refname:short)", "refs/remotes")
	assert.NotContains(t, remoteTrackingBranches, "3-fix-the-login-form")
	assert.NotContains(t, remoteTrackingBranches, "5-add-dark-mode")

	assert.EqualError(t, f.app.DeleteBranch("master"), "can't delete the checked-out branch 'master'")
}

func TestDeleteIssue(t *testing.T) {
	f := setup(t, &GitHubMock{Issues: []*GitHubMockIssue{
		{Number: 3, Title: "Fix the login form"},
		{Number: 5, Title: "Spam"},
	}}, false)

	require.NoError(t, f.app.DeleteIssue(5))

	assert.True(t, f.issue(5).Deleted)
	assert.False(t, f.issue(3).Deleted)
	assert.Equal(t, "Deleted issue #5\n", f.out.String())
}

func TestProjectIsLookedUpOnceAndRefreshedForUnknownStatuses(t *testing.T) {
	f := setup(t, &GitHubMock{
		Issues:        []*GitHubMockIssue{{Number: 3, Title: "Fix the login form"}},
		StatusOptions: boardStatuses,
		ProjectNumber: projectNumber,
	}, true)
	f.sh("checkout", "-q", "-b", "3-fix-the-login-form")

	require.NoError(t, f.app.SetStatus("Todo"))
	require.NoError(t, f.app.SetStatus("Done"))
	require.NoError(t, f.app.Statuses())
	assert.Equal(t, 1, f.mock.ProjectLookups)

	// A column added on GitHub isn't in the cache yet.
	f.mock.StatusOptions = append(f.mock.StatusOptions, "Blocked")
	require.NoError(t, f.app.SetStatus("Blocked"))
	assert.Equal(t, 2, f.mock.ProjectLookups)
	assert.Equal(t, "Blocked", f.issue(3).Status)
}
