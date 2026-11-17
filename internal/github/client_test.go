package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecodeGraphQLResponse(t *testing.T) {
	t.Run("decodes data", func(t *testing.T) {
		var out struct {
			Viewer struct{ ID string }
		}

		err := decodeGraphQLResponse([]byte(`{"data": {"viewer": {"id": "U_1"}}}`), &out)

		assert.NoError(t, err)
		assert.Equal(t, "U_1", out.Viewer.ID)
	})

	t.Run("reports a missing scope as ErrInsufficientScopes", func(t *testing.T) {
		resp := `{"errors": [{"type": "INSUFFICIENT_SCOPES", "message": "Your token has not been granted the required scopes to execute this query. The 'id' field requires one of the following scopes: ['read:project']"}]}`

		err := decodeGraphQLResponse([]byte(resp), nil)

		assert.ErrorIs(t, err, ErrInsufficientScopes)
	})

	t.Run("joins other error messages", func(t *testing.T) {
		resp := `{"errors": [{"type": "NOT_FOUND", "message": "first"}, {"message": "second"}]}`

		err := decodeGraphQLResponse([]byte(resp), nil)

		assert.EqualError(t, err, "first\nsecond")
	})
}

func TestProjectStatusOptionID(t *testing.T) {
	project := &Project{Options: []ProjectOption{{ID: "opt_1", Name: "In Progress"}}}

	id, ok := project.StatusOptionID("In Progress")
	assert.True(t, ok)
	assert.Equal(t, "opt_1", id)

	_, ok = project.StatusOptionID("Done")
	assert.False(t, ok)
}

func TestIsApprovedByOtherThan(t *testing.T) {
	cases := []struct {
		name       string
		approvedBy []string
		expected   bool
	}{
		{"nobody approved", nil, false},
		{"only the viewer approved", []string{"me"}, false},
		{"the viewer approved, with different case", []string{"ME"}, false},
		{"someone else approved", []string{"me", "reviewer"}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pr := &OpenPullRequest{ApprovedBy: c.approvedBy}
			assert.Equal(t, c.expected, pr.IsApprovedByOtherThan("me"))
		})
	}
}
