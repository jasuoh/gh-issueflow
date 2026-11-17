package github

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/samber/lo"
)

// ErrInsufficientScopes is returned when the token lacks a scope that a
// request needs; for the projects API that is the `project` scope.
var ErrInsufficientScopes = errors.New("the GitHub token is missing a required scope")

// Client sends GraphQL requests concerning one GitHub repository.
type Client struct {
	endpoint   string
	httpClient *http.Client
	owner      string
	repo       string
}

// NewClient returns a client that sends its requests to endpoint through
// httpClient, which is responsible for authenticating them.
func NewClient(endpoint string, httpClient *http.Client, owner string, repo string) *Client {
	return &Client{endpoint: endpoint, httpClient: httpClient, owner: owner, repo: repo}
}

// Endpoint returns the GraphQL API URL for a GitHub host. github.com uses a
// dedicated api. subdomain; GitHub Enterprise Server hangs the API off the
// web host under /api/graphql.
func Endpoint(host string) string {
	if strings.EqualFold(host, "github.com") {
		return "https://api.github.com/graphql"
	}
	return "https://" + host + "/api/graphql"
}

func (self *Client) Owner() string {
	return self.owner
}

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

// run sends a query or mutation and decodes its data into out.
func (self *Client) run(query string, variables map[string]any, out any) error {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return err
	}
	resp, err := self.httpClient.Post(self.endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API request failed with status %s: %s", resp.Status, respBytes)
	}
	return decodeGraphQLResponse(respBytes, out)
}

func decodeGraphQLResponse(respBytes []byte, out any) error {
	var resp graphQLResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return err
	}

	if len(resp.Errors) > 0 {
		if lo.SomeBy(resp.Errors, func(e graphQLError) bool { return e.Type == "INSUFFICIENT_SCOPES" }) {
			return ErrInsufficientScopes
		}
		messages := lo.Map(resp.Errors, func(e graphQLError, _ int) string { return e.Message })
		return errors.New(strings.Join(messages, "\n"))
	}

	if out == nil {
		return nil
	}
	return json.Unmarshal(resp.Data, out)
}

type Repository struct {
	ID            string
	DefaultBranch string
	// The commit the default branch points at, which linked branches are
	// created from.
	DefaultBranchOid string
	ViewerID         string
	ViewerLogin      string
}

func (self *Client) Repository() (*Repository, error) {
	const query = `query IssueflowRepository($owner: String!, $repo: String!) {
  viewer { id login }
  repository(owner: $owner, name: $repo) {
    id
    defaultBranchRef { name target { oid } }
  }
}`
	var data struct {
		Viewer     struct{ ID, Login string }
		Repository *struct {
			ID               string
			DefaultBranchRef *struct {
				Name   string
				Target struct{ Oid string }
			}
		}
	}
	if err := self.run(query, map[string]any{"owner": self.owner, "repo": self.repo}, &data); err != nil {
		return nil, err
	}
	if data.Repository == nil {
		return nil, fmt.Errorf("repository %s/%s not found", self.owner, self.repo)
	}

	result := &Repository{ID: data.Repository.ID, ViewerID: data.Viewer.ID, ViewerLogin: data.Viewer.Login}
	if ref := data.Repository.DefaultBranchRef; ref != nil {
		result.DefaultBranch = ref.Name
		result.DefaultBranchOid = ref.Target.Oid
	}
	return result, nil
}

type Issue struct {
	ID     string
	Number int
	Title  string
	Url    string
}

func (self *Client) OpenIssues() ([]*Issue, error) {
	const query = `query IssueflowOpenIssues($owner: String!, $repo: String!) {
  repository(owner: $owner, name: $repo) {
    issues(first: 100, states: OPEN, orderBy: {field: CREATED_AT, direction: DESC}) {
      nodes { id number title url }
    }
  }
}`
	var data struct {
		Repository struct {
			Issues struct{ Nodes []*Issue }
		}
	}
	if err := self.run(query, map[string]any{"owner": self.owner, "repo": self.repo}, &data); err != nil {
		return nil, err
	}
	return data.Repository.Issues.Nodes, nil
}

func (self *Client) Issue(number int) (*Issue, error) {
	const query = `query IssueflowIssue($owner: String!, $repo: String!, $number: Int!) {
  repository(owner: $owner, name: $repo) {
    issue(number: $number) { id number title url }
  }
}`
	var data struct {
		Repository struct{ Issue *Issue }
	}
	if err := self.run(query, map[string]any{"owner": self.owner, "repo": self.repo, "number": number}, &data); err != nil {
		return nil, err
	}
	if data.Repository.Issue == nil {
		return nil, fmt.Errorf("issue #%d not found", number)
	}
	return data.Repository.Issue, nil
}

func (self *Client) AssignIssue(issueID string, userID string) error {
	const query = `mutation IssueflowAssignIssue($issueId: ID!, $userId: ID!) {
  addAssigneesToAssignable(input: {assignableId: $issueId, assigneeIds: [$userId]}) { clientMutationId }
}`
	return self.run(query, map[string]any{"issueId": issueID, "userId": userID}, nil)
}

// CreateLinkedBranch creates a branch on GitHub that is linked to the issue
// (it shows up in the issue's Development section) and returns its name.
func (self *Client) CreateLinkedBranch(issueID string, repo *Repository, name string) (string, error) {
	const query = `mutation IssueflowCreateLinkedBranch($issueId: ID!, $repositoryId: ID!, $oid: GitObjectID!, $name: String!) {
  createLinkedBranch(input: {issueId: $issueId, repositoryId: $repositoryId, oid: $oid, name: $name}) {
    linkedBranch { ref { name } }
  }
}`
	var data struct {
		CreateLinkedBranch struct {
			LinkedBranch *struct {
				Ref struct{ Name string }
			}
		}
	}
	variables := map[string]any{
		"issueId":      issueID,
		"repositoryId": repo.ID,
		"oid":          repo.DefaultBranchOid,
		"name":         name,
	}
	if err := self.run(query, variables, &data); err != nil {
		return "", err
	}
	if data.CreateLinkedBranch.LinkedBranch == nil {
		return "", fmt.Errorf("GitHub did not create the branch %s", name)
	}
	return data.CreateLinkedBranch.LinkedBranch.Ref.Name, nil
}

// Project holds the IDs needed to set an item's status on a project
// (v2) board.
type Project struct {
	ID            string          `json:"id"`
	StatusFieldID string          `json:"statusFieldId"`
	Options       []ProjectOption `json:"options"` // in board order
}

type ProjectOption struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// StatusOptions returns the names of the status options, in board order.
func (self *Project) StatusOptions() []string {
	return lo.Map(self.Options, func(option ProjectOption, _ int) string { return option.Name })
}

// StatusOptionID returns the ID of the status option with the given name.
// Names are matched case-insensitively, since they are typed into the config
// by hand.
func (self *Project) StatusOptionID(name string) (string, bool) {
	option, ok := lo.Find(self.Options, func(option ProjectOption) bool { return strings.EqualFold(option.Name, name) })
	return option.ID, ok
}

// Project looks up a project of the given user or organization, together with
// its single-select field called statusField.
func (self *Client) Project(owner string, number int, statusField string) (*Project, error) {
	const query = `query IssueflowProject($owner: String!, $number: Int!, $field: String!) {
  repositoryOwner(login: $owner) {
    ... on ProjectV2Owner {
      projectV2(number: $number) {
        id
        field(name: $field) {
          ... on ProjectV2SingleSelectField { id options { id name } }
        }
      }
    }
  }
}`
	var data struct {
		RepositoryOwner *struct {
			ProjectV2 *struct {
				ID    string
				Field *struct {
					ID      string
					Options []struct{ ID, Name string }
				}
			}
		}
	}
	variables := map[string]any{"owner": owner, "number": number, "field": statusField}
	if err := self.run(query, variables, &data); err != nil {
		return nil, err
	}
	if data.RepositoryOwner == nil || data.RepositoryOwner.ProjectV2 == nil {
		return nil, fmt.Errorf("project %d of %s not found", number, owner)
	}
	project := data.RepositoryOwner.ProjectV2
	if project.Field == nil || project.Field.ID == "" {
		return nil, fmt.Errorf("project %d of %s has no single-select field named %q", number, owner, statusField)
	}

	result := &Project{ID: project.ID, StatusFieldID: project.Field.ID}
	for _, option := range project.Field.Options {
		result.Options = append(result.Options, ProjectOption{ID: option.ID, Name: option.Name})
	}
	return result, nil
}

// AddToProject adds an issue or pull request to the project and returns the
// ID of its project item. GitHub returns the existing item if the content is
// already on the board, so this doubles as the item lookup.
func (self *Client) AddToProject(projectID string, contentID string) (string, error) {
	const query = `mutation IssueflowAddToProject($projectId: ID!, $contentId: ID!) {
  addProjectV2ItemById(input: {projectId: $projectId, contentId: $contentId}) { item { id } }
}`
	var data struct {
		AddProjectV2ItemById struct {
			Item struct{ ID string }
		}
	}
	if err := self.run(query, map[string]any{"projectId": projectID, "contentId": contentID}, &data); err != nil {
		return "", err
	}
	return data.AddProjectV2ItemById.Item.ID, nil
}

func (self *Client) SetProjectItemStatus(project *Project, itemID string, optionID string) error {
	const query = `mutation IssueflowSetProjectItemStatus($projectId: ID!, $itemId: ID!, $fieldId: ID!, $optionId: String!) {
  updateProjectV2ItemFieldValue(input: {projectId: $projectId, itemId: $itemId, fieldId: $fieldId, value: {singleSelectOptionId: $optionId}}) {
    projectV2Item { id }
  }
}`
	variables := map[string]any{
		"projectId": project.ID,
		"itemId":    itemID,
		"fieldId":   project.StatusFieldID,
		"optionId":  optionID,
	}
	return self.run(query, variables, nil)
}

func (self *Client) CreateIssue(repo *Repository, title string) (*Issue, error) {
	const query = `mutation IssueflowCreateIssue($repositoryId: ID!, $title: String!, $assigneeIds: [ID!]) {
  createIssue(input: {repositoryId: $repositoryId, title: $title, assigneeIds: $assigneeIds}) {
    issue { id number title url }
  }
}`
	var data struct {
		CreateIssue struct{ Issue Issue }
	}
	variables := map[string]any{
		"repositoryId": repo.ID,
		"title":        title,
		"assigneeIds":  []string{repo.ViewerID},
	}
	if err := self.run(query, variables, &data); err != nil {
		return nil, err
	}
	return &data.CreateIssue.Issue, nil
}

type CreatedPullRequest struct {
	ID     string
	Number int
	Url    string
}

// CreatePullRequest opens a pull request from head into base. head is a
// branch name, prefixed with "<owner>:" when the branch lives in a fork.
func (self *Client) CreatePullRequest(repo *Repository, base string, head string, title string, body string) (*CreatedPullRequest, error) {
	const query = `mutation IssueflowCreatePullRequest($repositoryId: ID!, $base: String!, $head: String!, $title: String!, $body: String!) {
  createPullRequest(input: {repositoryId: $repositoryId, baseRefName: $base, headRefName: $head, title: $title, body: $body}) {
    pullRequest { id number url }
  }
}`
	var data struct {
		CreatePullRequest struct{ PullRequest CreatedPullRequest }
	}
	variables := map[string]any{
		"repositoryId": repo.ID,
		"base":         base,
		"head":         head,
		"title":        title,
		"body":         body,
	}
	if err := self.run(query, variables, &data); err != nil {
		return nil, err
	}
	return &data.CreatePullRequest.PullRequest, nil
}

type OpenPullRequest struct {
	ID     string
	Number int
	Url    string
	// The logins of the reviewers whose latest review approves the pull
	// request.
	ApprovedBy []string
}

// IsApprovedByOtherThan tells whether someone other than the given user
// approved the pull request.
func (self *OpenPullRequest) IsApprovedByOtherThan(login string) bool {
	return lo.SomeBy(self.ApprovedBy, func(approver string) bool { return !strings.EqualFold(approver, login) })
}

// OpenPullRequestForBranch returns the open pull request whose head is the
// given branch, or nil if there is none.
func (self *Client) OpenPullRequestForBranch(branch string) (*OpenPullRequest, error) {
	const query = `query IssueflowOpenPullRequestForBranch($owner: String!, $repo: String!, $branch: String!) {
  repository(owner: $owner, name: $repo) {
    pullRequests(headRefName: $branch, states: OPEN, first: 1) {
      nodes {
        id number url
        latestOpinionatedReviews(first: 100) { nodes { state author { login } } }
      }
    }
  }
}`
	var data struct {
		Repository struct {
			PullRequests struct {
				Nodes []struct {
					ID                       string
					Number                   int
					Url                      string
					LatestOpinionatedReviews struct {
						Nodes []struct {
							State  string
							Author *struct{ Login string }
						}
					}
				}
			}
		}
	}
	variables := map[string]any{"owner": self.owner, "repo": self.repo, "branch": branch}
	if err := self.run(query, variables, &data); err != nil {
		return nil, err
	}
	nodes := data.Repository.PullRequests.Nodes
	if len(nodes) == 0 {
		return nil, nil
	}

	node := nodes[0]
	result := &OpenPullRequest{ID: node.ID, Number: node.Number, Url: node.Url}
	for _, review := range node.LatestOpinionatedReviews.Nodes {
		// The author is null for reviews by deleted accounts.
		if review.State == "APPROVED" && review.Author != nil {
			result.ApprovedBy = append(result.ApprovedBy, review.Author.Login)
		}
	}
	return result, nil
}

func (self *Client) SquashMergePullRequest(pullRequestID string) error {
	const query = `mutation IssueflowSquashMergePullRequest($pullRequestId: ID!) {
  mergePullRequest(input: {pullRequestId: $pullRequestId, mergeMethod: SQUASH}) { clientMutationId }
}`
	return self.run(query, map[string]any{"pullRequestId": pullRequestID}, nil)
}

func (self *Client) DeleteIssue(issueID string) error {
	const query = `mutation IssueflowDeleteIssue($issueId: ID!) {
  deleteIssue(input: {issueId: $issueId}) { clientMutationId }
}`
	return self.run(query, map[string]any{"issueId": issueID}, nil)
}
