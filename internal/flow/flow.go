// Package flow implements the subcommands of gh-issueflow.
package flow

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/jasuoh/gh-issueflow/internal/git"
	"github.com/jasuoh/gh-issueflow/internal/github"
	"github.com/jasuoh/gh-issueflow/internal/naming"
)

const missingScopeMessage = "your GitHub token lacks the 'project' scope needed for project boards. Run: gh auth refresh -s project"

// App holds what every subcommand works with.
type App struct {
	Git    *git.Repo
	Client *github.Client
	// The host of the GitHub repository, for keying the project cache.
	Host string
	// The name of the git remote that points at the GitHub repository.
	Remote string
	Config Config
	Cache  *ProjectCache
	Out    io.Writer
}

// TranslateError turns errors that the user can fix into instructions.
func TranslateError(err error) error {
	if errors.Is(err, github.ErrInsufficientScopes) {
		return errors.New(missingScopeMessage)
	}
	return err
}

func (self *App) printf(format string, args ...any) {
	fmt.Fprintf(self.Out, format, args...)
}

// Issues prints the open issues as "<number>\t<title>" lines, newest first.
func (self *App) Issues() error {
	issues, err := self.Client.OpenIssues()
	if err != nil {
		return err
	}
	for _, issue := range issues {
		self.printf("%d\t%s\n", issue.Number, issue.Title)
	}
	return nil
}

// Pick assigns the issue to the user, creates a branch linked to it on
// GitHub and checks it out, and marks the issue as in progress.
func (self *App) Pick(number int) error {
	// Resolve the project first, so that a misconfigured board or a missing
	// token scope is reported before anything has been changed on GitHub.
	board, err := self.board(self.Config.InProgress)
	if err != nil {
		return err
	}
	repo, err := self.Client.Repository()
	if err != nil {
		return err
	}
	issue, err := self.Client.Issue(number)
	if err != nil {
		return err
	}

	if err := self.Client.AssignIssue(issue.ID, repo.ViewerID); err != nil {
		return err
	}
	branch, err := self.Client.CreateLinkedBranch(issue.ID, repo, naming.IssueBranchName(issue.Number, issue.Title))
	if err != nil {
		return err
	}
	if err := board.setStatus(self.Client, issue.ID); err != nil {
		return err
	}

	if err := self.Git.FetchBranch(self.Remote, branch); err != nil {
		return err
	}
	if err := self.Git.CheckoutNewTrackingBranch(branch, self.Remote); err != nil {
		return err
	}

	self.printf("Picked #%d: checked out %s\n", issue.Number, branch)
	return nil
}

// New creates an issue assigned to the user and puts it on the board.
func (self *App) New(title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("the issue title must not be empty")
	}

	project, err := self.project()
	if err != nil {
		return err
	}
	repo, err := self.Client.Repository()
	if err != nil {
		return err
	}

	issue, err := self.Client.CreateIssue(repo, title)
	if err != nil {
		return err
	}
	if project != nil {
		if _, err := self.Client.AddToProject(project.ID, issue.ID); err != nil {
			return err
		}
	}

	self.printf("Created issue #%d: %s\n", issue.Number, issue.Url)
	return nil
}

// PullRequest pushes the current branch and opens a pull request for it
// against the default branch, closing the issue the branch is named after
// and marking that issue as in review.
func (self *App) PullRequest() error {
	branch, err := self.Git.CurrentBranch()
	if err != nil {
		return err
	}
	board, err := self.board(self.Config.InReview)
	if err != nil {
		return err
	}
	repo, err := self.Client.Repository()
	if err != nil {
		return err
	}
	if branch == repo.DefaultBranch {
		return fmt.Errorf("can't open a pull request from the default branch '%s'", branch)
	}

	var issue *github.Issue
	if number, ok := naming.IssueNumberFromBranchName(branch); ok {
		if issue, err = self.Client.Issue(number); err != nil {
			return err
		}
	}

	title, err := self.pullRequestTitle(repo, branch, issue)
	if err != nil {
		return err
	}

	remote, remoteBranch := self.Git.Upstream(branch)
	if remote == "" {
		remote, remoteBranch = self.Remote, branch
	}
	if err := self.Git.PushWithUpstream(remote, branch, remoteBranch); err != nil {
		return err
	}

	head, err := self.pullRequestHead(remote, remoteBranch)
	if err != nil {
		return err
	}
	body := ""
	if issue != nil {
		body = fmt.Sprintf("Closes #%d", issue.Number)
	}

	pr, err := self.Client.CreatePullRequest(repo, repo.DefaultBranch, head, title, body)
	if err != nil {
		return err
	}
	if issue != nil {
		if err := board.setStatus(self.Client, issue.ID); err != nil {
			return err
		}
	}

	self.printf("Created pull request #%d:\n%s\n", pr.Number, pr.Url)
	return nil
}

// pullRequestTitle follows what `gh pr create --fill` does: a single commit
// gives the pull request its subject; for several, the issue's title says
// best what they add up to, falling back to the branch name.
func (self *App) pullRequestTitle(repo *github.Repository, branch string, issue *github.Issue) (string, error) {
	base := self.Remote + "/" + repo.DefaultBranch
	subjects, err := self.Git.CommitSubjects(base, branch)
	if err != nil {
		return "", err
	}

	switch {
	case len(subjects) == 0:
		return "", fmt.Errorf("the branch '%s' has no commits that aren't on '%s'", branch, base)
	case len(subjects) == 1:
		return subjects[0], nil
	case issue != nil:
		return issue.Title, nil
	default:
		return strings.NewReplacer("-", " ", "_", " ").Replace(branch), nil
	}
}

// pullRequestHead returns the head to open a pull request from: the branch
// name, qualified with its owner when it was pushed to a fork rather than to
// the base repository.
func (self *App) pullRequestHead(remote string, remoteBranch string) (string, error) {
	if remote == self.Remote {
		return remoteBranch, nil
	}
	urls, err := self.Git.RemoteURLs()
	if err != nil {
		return "", err
	}
	owner, _, ok := ParseRemoteURL(urls[remote])
	if !ok || strings.EqualFold(owner, self.Client.Owner()) {
		return remoteBranch, nil
	}
	return owner + ":" + remoteBranch, nil
}

// Statuses prints the options of the board's status field, one per line.
func (self *App) Statuses() error {
	project, err := self.requireProject()
	if err != nil {
		return err
	}
	for _, status := range project.StatusOptions() {
		self.printf("%s\n", status)
	}
	return nil
}

// SetStatus sets the status of the issue that the current branch is named
// after.
func (self *App) SetStatus(status string) error {
	branch, err := self.Git.CurrentBranch()
	if err != nil {
		return err
	}
	number, ok := naming.IssueNumberFromBranchName(branch)
	if !ok {
		return fmt.Errorf("the branch '%s' isn't named after an issue; its name must start with the issue number", branch)
	}
	if _, err := self.requireProject(); err != nil {
		return err
	}
	board, err := self.board(status)
	if err != nil {
		return err
	}
	issue, err := self.Client.Issue(number)
	if err != nil {
		return err
	}
	if err := board.setStatus(self.Client, issue.ID); err != nil {
		return err
	}

	self.printf("Set status of #%d to '%s'\n", number, status)
	return nil
}

// Merge squash-merges the pull request of the current branch, deletes the
// branch on the remote and locally, and checks out the default branch. The
// issue the branch is named after is marked as done, but only if someone
// other than the user approved the pull request.
func (self *App) Merge() error {
	branch, err := self.Git.CurrentBranch()
	if err != nil {
		return err
	}
	board, err := self.board(self.Config.Done)
	if err != nil {
		return err
	}
	repo, err := self.Client.Repository()
	if err != nil {
		return err
	}
	if branch == repo.DefaultBranch {
		return fmt.Errorf("can't merge the default branch '%s'", branch)
	}

	remote, remoteBranch := self.Git.Upstream(branch)
	if remote == "" {
		remote, remoteBranch = self.Remote, branch
	}
	pr, err := self.Client.OpenPullRequestForBranch(remoteBranch)
	if err != nil {
		return err
	}
	if pr == nil {
		return fmt.Errorf("there is no open pull request for the branch '%s'", branch)
	}

	issueNumber, hasIssue := naming.IssueNumberFromBranchName(branch)
	tracksIssue := board != nil && hasIssue
	approved := pr.IsApprovedByOtherThan(repo.ViewerLogin)

	if err := self.Client.SquashMergePullRequest(pr.ID); err != nil {
		return err
	}
	if tracksIssue && approved {
		issue, err := self.Client.Issue(issueNumber)
		if err != nil {
			return err
		}
		if err := board.setStatus(self.Client, issue.ID); err != nil {
			return err
		}
	}

	if err := self.deleteBranch(branch, remote, remoteBranch, repo.DefaultBranch); err != nil {
		return err
	}

	self.printf("Merged pull request #%d\n", pr.Number)
	if tracksIssue && !approved {
		self.printf("The issue's status is unchanged, because nobody else approved the pull request.\n")
	}
	return nil
}

// Branches prints the local branches other than the checked-out one.
func (self *App) Branches() error {
	current, _ := self.Git.CurrentBranch()
	branches, err := self.Git.LocalBranches()
	if err != nil {
		return err
	}
	for _, branch := range branches {
		if branch != current {
			self.printf("%s\n", branch)
		}
	}
	return nil
}

// DeleteBranch deletes a branch other than the checked-out one, locally and
// on the remote it tracks. A remote branch that is gone already isn't an
// error.
func (self *App) DeleteBranch(branch string) error {
	if current, _ := self.Git.CurrentBranch(); branch == current {
		return fmt.Errorf("can't delete the checked-out branch '%s'", branch)
	}
	remote, remoteBranch := self.Git.Upstream(branch)
	if err := self.deleteBranch(branch, remote, remoteBranch, ""); err != nil {
		return err
	}

	self.printf("Deleted branch %s\n", branch)
	return nil
}

// deleteBranch deletes the branch on the remote, if it tracks one, and
// locally, after checking out switchTo if that isn't empty.
func (self *App) deleteBranch(branch string, remote string, remoteBranch string, switchTo string) error {
	if remote != "" {
		if err := self.Git.DeleteRemoteBranch(remote, remoteBranch); err != nil {
			return err
		}
		self.Git.PruneRemoteTrackingBranch(remote, remoteBranch)
	}
	if switchTo != "" {
		if err := self.Git.Checkout(switchTo); err != nil {
			return err
		}
	}
	return self.Git.DeleteLocalBranch(branch)
}

// DeleteIssue permanently deletes an issue.
func (self *App) DeleteIssue(number int) error {
	issue, err := self.Client.Issue(number)
	if err != nil {
		return err
	}
	if err := self.Client.DeleteIssue(issue.ID); err != nil {
		return err
	}

	self.printf("Deleted issue #%d\n", number)
	return nil
}

// ParseIssueNumber parses an issue number as given on the command line,
// with or without a leading '#'.
func ParseIssueNumber(arg string) (int, error) {
	number, err := strconv.Atoi(strings.TrimPrefix(arg, "#"))
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("'%s' isn't an issue number", arg)
	}
	return number, nil
}
