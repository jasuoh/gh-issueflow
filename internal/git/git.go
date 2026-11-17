// Package git runs the git commands that the workflows need.
package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Repo runs git in one working directory.
type Repo struct {
	Dir string
}

func (self *Repo) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = self.Dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func lines(output string) []string {
	if output == "" {
		return nil
	}
	return strings.Split(output, "\n")
}

// CurrentBranch returns the checked-out branch, or an error if HEAD is
// detached.
func (self *Repo) CurrentBranch() (string, error) {
	name, err := self.run("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", errors.New("no branch is checked out")
	}
	return name, nil
}

// LocalBranches returns the names of all local branches.
func (self *Repo) LocalBranches() ([]string, error) {
	output, err := self.run("for-each-ref", "--format=%(refname:short)", "refs/heads")
	return lines(output), err
}

// Upstream returns the remote and the remote branch that the branch tracks,
// or empty strings if it tracks none.
func (self *Repo) Upstream(branch string) (remote string, remoteBranch string) {
	remote, _ = self.run("config", "--get", "branch."+branch+".remote")
	merge, _ := self.run("config", "--get", "branch."+branch+".merge")
	if remote == "" || remote == "." || merge == "" {
		return "", ""
	}
	return remote, strings.TrimPrefix(merge, "refs/heads/")
}

// RemoteURLs returns the fetch URL of every remote, keyed by remote name.
func (self *Repo) RemoteURLs() (map[string]string, error) {
	output, err := self.run("remote")
	if err != nil {
		return nil, err
	}
	urls := map[string]string{}
	for _, name := range lines(output) {
		url, err := self.run("config", "--get", "remote."+name+".url")
		if err == nil {
			urls[name] = url
		}
	}
	return urls, nil
}

// Config returns the value of a git config key, or "" if it isn't set.
func (self *Repo) Config(key string) string {
	value, _ := self.run("config", "--get", key)
	return value
}

func (self *Repo) FetchBranch(remote string, branch string) error {
	_, err := self.run("fetch", remote, fmt.Sprintf("+refs/heads/%s:refs/remotes/%s/%s", branch, remote, branch))
	return err
}

// CheckoutNewTrackingBranch creates a local branch tracking the remote one
// and checks it out.
func (self *Repo) CheckoutNewTrackingBranch(branch string, remote string) error {
	_, err := self.run("checkout", "-b", branch, "--track", remote+"/"+branch)
	return err
}

func (self *Repo) Checkout(branch string) error {
	_, err := self.run("checkout", branch)
	return err
}

// PushWithUpstream pushes branch to remoteBranch on remote and makes it the
// branch's upstream.
func (self *Repo) PushWithUpstream(remote string, branch string, remoteBranch string) error {
	_, err := self.run("push", "--set-upstream", remote, "refs/heads/"+branch+":refs/heads/"+remoteBranch)
	return err
}

// CommitSubjects returns the subjects of the commits on to that aren't on
// from, oldest first.
func (self *Repo) CommitSubjects(from string, to string) ([]string, error) {
	output, err := self.run("-c", "log.showsignature=false", "log", "--format=%s", "--reverse", from+".."+to)
	return lines(output), err
}

// RemoteHasBranch asks the remote whether it has the branch.
func (self *Repo) RemoteHasBranch(remote string, branch string) (bool, error) {
	output, err := self.run("ls-remote", "--heads", remote, "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	return output != "", nil
}

// DeleteRemoteBranch deletes the branch on the remote, unless it is gone
// already (e.g. because GitHub deletes head branches on merge).
func (self *Repo) DeleteRemoteBranch(remote string, branch string) error {
	exists, err := self.RemoteHasBranch(remote, branch)
	if err != nil || !exists {
		return err
	}
	_, err = self.run("push", remote, "--delete", "refs/heads/"+branch)
	return err
}

// DeleteLocalBranch deletes the branch even if it isn't merged, since
// squash-merged branches never are as far as git can tell.
func (self *Repo) DeleteLocalBranch(branch string) error {
	_, err := self.run("branch", "-D", branch)
	return err
}

// PruneRemoteTrackingBranch removes the remote-tracking branch, which
// deleting the remote branch through a push doesn't do when GitHub deleted
// it on its own.
func (self *Repo) PruneRemoteTrackingBranch(remote string, branch string) {
	_, _ = self.run("update-ref", "-d", "refs/remotes/"+remote+"/"+branch)
}
