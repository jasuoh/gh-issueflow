package flow

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/samber/lo"
)

// A fake of the parts of GitHub's GraphQL API that gh-issueflow uses, for
// the tests.

const (
	GitHubMockOwner = "test-owner"
	GitHubMockRepo  = "test-repo"
	gitHubMockLogin = "test-user"
)

type GitHubMockIssue struct {
	Number    int
	Title     string
	Assignees []string
	// The issue's status on the project board, or "" if it isn't on the board.
	Status  string
	Deleted bool
}

type GitHubMockPullRequest struct {
	Number      int
	HeadRefName string
	BaseRefName string
	Title       string
	Body        string
	Merged      bool
	MergeMethod string
	// The logins of the reviewers whose latest review approves the pull
	// request. Not checked by PullRequest.
	ApprovedBy []string
}

func (self *GitHubMockPullRequest) Url() string {
	return fmt.Sprintf("https://github.com/%s/%s/pull/%d", GitHubMockOwner, GitHubMockRepo, self.Number)
}

// GitHubMock is a fake of the parts of GitHub's GraphQL API that lazygit
// uses. Integration tests start it from SetupConfig, which runs inside the
// lazygit process, so that it lives for as long as lazygit does and the
// test's Run function can inspect what lazygit did to it.
type GitHubMock struct {
	mutex sync.Mutex

	Issues       []*GitHubMockIssue
	PullRequests []*GitHubMockPullRequest
	// The options of the project's status field. Nil means the project
	// doesn't exist.
	StatusOptions []string
	ProjectNumber int
	// Makes every projects request fail like it does for a token without the
	// `project` scope.
	MissingProjectScope bool
	// How many project lookups were made, to check the cache.
	ProjectLookups int

	remotePath string
}

type gitHubMockRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

var operationNameRegex = regexp.MustCompile(`^\s*(?:query|mutation)\s+(\w+)`)

func (self *GitHubMock) serveHTTP(w http.ResponseWriter, r *http.Request) {
	var req gitHubMockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	operation := ""
	if match := operationNameRegex.FindStringSubmatch(req.Query); match != nil {
		operation = match[1]
	}

	self.mutex.Lock()
	data, errorType, err := self.handle(operation, req.Variables)
	self.mutex.Unlock()

	var resp map[string]any
	switch {
	case errorType != "":
		resp = map[string]any{"errors": []map[string]string{{"type": errorType, "message": err.Error()}}}
	case err != nil:
		resp = map[string]any{"errors": []map[string]string{{"message": err.Error()}}}
	default:
		resp = map[string]any{"data": data}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handle returns the data for a request, or an error, optionally with a
// GraphQL error type.
func (self *GitHubMock) handle(operation string, variables map[string]any) (any, string, error) {
	switch operation {
	case "IssueflowRepository":
		oid, err := self.remoteRevParse("master")
		if err != nil {
			return nil, "", err
		}
		return map[string]any{
			"viewer": map[string]any{"id": userNodeID(gitHubMockLogin), "login": gitHubMockLogin},
			"repository": map[string]any{
				"id":               "R_repo",
				"defaultBranchRef": map[string]any{"name": "master", "target": map[string]any{"oid": oid}},
			},
		}, "", nil

	case "IssueflowOpenIssues":
		open := lo.Filter(self.Issues, func(issue *GitHubMockIssue, _ int) bool { return !issue.Deleted })
		// Newest first, like the query asks for.
		slices.SortFunc(open, func(a, b *GitHubMockIssue) int { return b.Number - a.Number })
		nodes := lo.Map(open, func(issue *GitHubMockIssue, _ int) map[string]any { return issueNode(issue) })
		return map[string]any{"repository": map[string]any{"issues": map[string]any{"nodes": nodes}}}, "", nil

	case "IssueflowIssue":
		issue := self.issueByNumber(intVariable(variables, "number"))
		var node any
		if issue != nil {
			node = issueNode(issue)
		}
		return map[string]any{"repository": map[string]any{"issue": node}}, "", nil

	case "IssueflowAssignIssue":
		issue, err := self.issueByNodeID(variables["issueId"])
		if err != nil {
			return nil, "", err
		}
		if !slices.Contains(issue.Assignees, gitHubMockLogin) {
			issue.Assignees = append(issue.Assignees, gitHubMockLogin)
		}
		return map[string]any{"addAssigneesToAssignable": map[string]any{"clientMutationId": nil}}, "", nil

	case "IssueflowCreateLinkedBranch":
		if _, err := self.issueByNodeID(variables["issueId"]); err != nil {
			return nil, "", err
		}
		name := variables["name"].(string)
		if err := self.remoteGit("branch", name, variables["oid"].(string)); err != nil {
			return nil, "", err
		}
		return map[string]any{
			"createLinkedBranch": map[string]any{"linkedBranch": map[string]any{"ref": map[string]any{"name": name}}},
		}, "", nil

	case "IssueflowProject":
		self.ProjectLookups++
		if self.MissingProjectScope {
			return nil, "INSUFFICIENT_SCOPES", fmt.Errorf("Your token has not been granted the required scopes to execute this query. The 'projectV2' field requires one of the following scopes: ['read:project']")
		}
		var project any
		if self.StatusOptions != nil && intVariable(variables, "number") == self.ProjectNumber {
			var field any
			if variables["field"] == "Status" {
				field = map[string]any{
					"id": "F_status",
					"options": lo.Map(self.StatusOptions, func(name string, _ int) map[string]any {
						return map[string]any{"id": optionNodeID(name), "name": name}
					}),
				}
			}
			project = map[string]any{"id": "PVT_project", "field": field}
		}
		return map[string]any{"repositoryOwner": map[string]any{"projectV2": project}}, "", nil

	case "IssueflowAddToProject":
		if self.MissingProjectScope {
			return nil, "INSUFFICIENT_SCOPES", fmt.Errorf("missing project scope")
		}
		issue, err := self.issueByNodeID(variables["contentId"])
		if err != nil {
			return nil, "", err
		}
		if issue.Status == "" {
			issue.Status = "(no status)"
		}
		return map[string]any{"addProjectV2ItemById": map[string]any{"item": map[string]any{"id": itemNodeID(issue)}}}, "", nil

	case "IssueflowSetProjectItemStatus":
		issue, ok := lo.Find(self.Issues, func(issue *GitHubMockIssue) bool { return itemNodeID(issue) == variables["itemId"] })
		if !ok {
			return nil, "NOT_FOUND", fmt.Errorf("no project item %v", variables["itemId"])
		}
		status, ok := lo.Find(self.StatusOptions, func(name string) bool { return optionNodeID(name) == variables["optionId"] })
		if !ok {
			return nil, "", fmt.Errorf("no status option %v", variables["optionId"])
		}
		issue.Status = status
		return map[string]any{"updateProjectV2ItemFieldValue": map[string]any{"projectV2Item": map[string]any{"id": itemNodeID(issue)}}}, "", nil

	case "IssueflowCreateIssue":
		issue := &GitHubMockIssue{
			Number: self.nextNumber(),
			Title:  variables["title"].(string),
		}
		for _, id := range variables["assigneeIds"].([]any) {
			issue.Assignees = append(issue.Assignees, strings.TrimPrefix(id.(string), "U_"))
		}
		self.Issues = append(self.Issues, issue)
		return map[string]any{"createIssue": map[string]any{"issue": issueNode(issue)}}, "", nil

	case "IssueflowCreatePullRequest":
		head := variables["head"].(string)
		if _, err := self.remoteRevParse("refs/heads/" + head); err != nil {
			return nil, "", fmt.Errorf("Head sha can't be blank, Base sha can't be blank, No commits between master and %s", head)
		}
		pr := &GitHubMockPullRequest{
			Number:      self.nextNumber(),
			HeadRefName: head,
			BaseRefName: variables["base"].(string),
			Title:       variables["title"].(string),
			Body:        variables["body"].(string),
		}
		self.PullRequests = append(self.PullRequests, pr)
		return map[string]any{"createPullRequest": map[string]any{"pullRequest": pullRequestNode(pr)}}, "", nil

	case "IssueflowOpenPullRequestForBranch":
		var nodes []map[string]any
		pr, ok := lo.Find(self.PullRequests, func(pr *GitHubMockPullRequest) bool {
			return pr.HeadRefName == variables["branch"] && !pr.Merged
		})
		if ok {
			node := pullRequestNode(pr)
			node["latestOpinionatedReviews"] = map[string]any{
				"nodes": lo.Map(pr.ApprovedBy, func(login string, _ int) map[string]any {
					return map[string]any{"state": "APPROVED", "author": map[string]any{"login": login}}
				}),
			}
			nodes = append(nodes, node)
		}
		return map[string]any{"repository": map[string]any{"pullRequests": map[string]any{"nodes": nodes}}}, "", nil

	case "IssueflowSquashMergePullRequest":
		pr, ok := lo.Find(self.PullRequests, func(pr *GitHubMockPullRequest) bool { return pullRequestNodeID(pr) == variables["pullRequestId"] })
		if !ok || pr.Merged {
			return nil, "", fmt.Errorf("no open pull request %v", variables["pullRequestId"])
		}
		pr.Merged = true
		pr.MergeMethod = "SQUASH"
		return map[string]any{"mergePullRequest": map[string]any{"clientMutationId": nil}}, "", nil

	case "IssueflowDeleteIssue":
		issue, err := self.issueByNodeID(variables["issueId"])
		if err != nil {
			return nil, "", err
		}
		issue.Deleted = true
		return map[string]any{"deleteIssue": map[string]any{"clientMutationId": nil}}, "", nil

	default:
		// The pull request refresh, which these tests don't look at.
		return map[string]any{"repository": map[string]any{}}, "", nil
	}
}

// nextNumber returns the number for a new issue or pull request, which share
// one sequence on GitHub.
func (self *GitHubMock) nextNumber() int {
	maxIssue := lo.Max(lo.Map(self.Issues, func(issue *GitHubMockIssue, _ int) int { return issue.Number }))
	maxPullRequest := lo.Max(lo.Map(self.PullRequests, func(pr *GitHubMockPullRequest, _ int) int { return pr.Number }))
	return max(maxIssue, maxPullRequest) + 1
}

func (self *GitHubMock) issueByNumber(number int) *GitHubMockIssue {
	issue, _ := lo.Find(self.Issues, func(issue *GitHubMockIssue) bool { return issue.Number == number && !issue.Deleted })
	return issue
}

func (self *GitHubMock) issueByNodeID(id any) (*GitHubMockIssue, error) {
	issue, ok := lo.Find(self.Issues, func(issue *GitHubMockIssue) bool { return issueNodeID(issue) == id && !issue.Deleted })
	if !ok {
		return nil, fmt.Errorf("Could not resolve to a node with the global id of '%v'", id)
	}
	return issue, nil
}

func issueNode(issue *GitHubMockIssue) map[string]any {
	return map[string]any{
		"id":     issueNodeID(issue),
		"number": issue.Number,
		"title":  issue.Title,
		"url":    fmt.Sprintf("https://github.com/%s/%s/issues/%d", GitHubMockOwner, GitHubMockRepo, issue.Number),
	}
}

func pullRequestNode(pr *GitHubMockPullRequest) map[string]any {
	return map[string]any{"id": pullRequestNodeID(pr), "number": pr.Number, "url": pr.Url()}
}

func pullRequestNodeID(pr *GitHubMockPullRequest) string { return "PR_" + strconv.Itoa(pr.Number) }

func issueNodeID(issue *GitHubMockIssue) string { return "I_" + strconv.Itoa(issue.Number) }
func itemNodeID(issue *GitHubMockIssue) string  { return "PVTI_" + strconv.Itoa(issue.Number) }
func userNodeID(login string) string            { return "U_" + login }

func optionNodeID(name string) string {
	return "O_" + strings.ReplaceAll(strings.ToLower(name), " ", "_")
}

func intVariable(variables map[string]any, name string) int {
	// JSON numbers decode as float64.
	value, _ := variables[name].(float64)
	return int(value)
}

func (self *GitHubMock) remoteGit(args ...string) error {
	output, err := exec.Command("git", append([]string{"-C", self.remotePath}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return nil
}

func (self *GitHubMock) remoteRevParse(ref string) (string, error) {
	output, err := exec.Command("git", "-C", self.remotePath, "rev-parse", ref).Output()
	return strings.TrimSpace(string(output)), err
}
