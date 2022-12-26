package github

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"

	gogithub "github.com/google/go-github/v48/github"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/uServers/miniprow/pkg/github/internal"
	"golang.org/x/oauth2"
	"sigs.k8s.io/release-utils/env"
)

const (
	// TokenEnvKey is the default GitHub token environemt variable key
	TokenEnvKey = "GITHUB_TOKEN"
	// GitHubURL Prefix for github URLs
	GitHubURL = "https://github.com/"
)

// GitHub is a wrapper around GitHub related functionality
type GitHub struct {
	client  Client
	options *Options
}

type githubClient struct {
	*gogithub.Client
}

type Client interface {
	GetComment(
		ctx context.Context, owner, repo string, number int64,
	) (*gogithub.IssueComment, *gogithub.Response, error)

	GetIssue(
		context.Context, string, string, int,
	) (*gogithub.Issue, *gogithub.Response, error)

	GetPullRequest(
		context.Context, string, string, int,
	) (*gogithub.PullRequest, *gogithub.Response, error)

	ListLabels(
		context.Context, string, string, *Options,
	) ([]*gogithub.Label, error)

	AddLabel(
		context.Context, string, string, int, string,
	) error

	RemoveLabel(
		context.Context, string, string, int, string,
	) error

	MergePullRequest(context.Context, string, string, int) error

	ListPullRequestFiles(
		context.Context, string, string, int, *gogithub.ListOptions,
	) ([]*gogithub.CommitFile, error)

	ListCheckRunsForRef(
		context.Context, string, string, string, *gogithub.ListCheckRunsOptions,
	) (*gogithub.ListCheckRunsResults, error)

	GetIssueComments(
		context.Context, string, string, int, *gogithub.IssueListCommentsOptions,
	) ([]*gogithub.IssueComment, error)

	GetAPIUser(ctx context.Context) (user *gogithub.User, err error)

	CreateComment(
		context.Context, string, string, int, string,
	) (*gogithub.IssueComment, error)

	DeleteComment(context.Context, string, string, int64) (err error)
}

// Options is a set of options to configure the behavior of the GitHub package
type Options struct {
	// How many items to request in calls to the github API
	// that require pagination.
	ItemsPerPage int
}

func (o *Options) GetItemsPerPage() int {
	return o.ItemsPerPage
}

// DefaultOptions return an options struct with commonly used settings
func DefaultOptions() *Options {
	return &Options{
		ItemsPerPage: 50,
	}
}

func New() *GitHub {
	token := env.Default(TokenEnvKey, "")
	client, _ := NewWithToken(token) // nolint: errcheck
	return client
}

// NewWithToken can be used to specify a GitHub token through parameters.
// Empty string will result in unauthenticated client, which makes
// unauthenticated requests.
func NewWithToken(token string) (*GitHub, error) {
	ctx := context.Background()
	client := http.DefaultClient
	state := "unauthenticated"
	if token != "" {
		state = strings.TrimPrefix(state, "un")
		client = oauth2.NewClient(ctx, oauth2.StaticTokenSource(
			&oauth2.Token{AccessToken: token},
		))
	}
	logrus.Debugf("Using %s GitHub client", state)
	return &GitHub{
		client:  &githubClient{gogithub.NewClient(client)},
		options: DefaultOptions(),
	}, nil
}

var MaxGithubRetries = 3

// Lists the labels in a given repository
func (github *GitHub) ListLabels(owner, repo string) ([]*gogithub.Label, error) {
	labels, err := github.client.ListLabels(context.Background(), owner, repo, github.options)
	if err != nil {
		return nil, errors.Wrap(err, "getting labels from repo")
	}
	return labels, nil
}

func (github *GitHub) AddLabel(owner, repo string, issue int, label string) error {
	if err := github.client.AddLabel(context.Background(), owner, repo, issue, label); err != nil {
		return errors.Wrap(err, "added label "+label)
	}
	return nil
}

func (github *GitHub) RemoveLabel(owner, repo string, issue int, label string) error {
	if err := github.client.RemoveLabel(context.Background(), owner, repo, issue, label); err != nil {
		return errors.Wrap(err, "removed label "+label)
	}
	return nil
}

func (github *GitHub) GetComment(owner, repo string, commentID int64) (comment *gogithub.IssueComment, err error) {
	comment, _, err = github.client.GetComment(context.Background(), owner, repo, commentID)
	return comment, err
}

func (github *GitHub) MergePullRequest(ctx context.Context, owner string, repo string, number int) error {
	return github.client.MergePullRequest(ctx, owner, repo, number)
}

func (g *githubClient) MergePullRequest(
	ctx context.Context, owner string, repo string, number int,
) error {
	// One default message for the merge commit
	msg := fmt.Sprintf("MiniProw: merge pull request #%d", number)

	// Call the GitHub API to merge the PR
	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		_, r, err := g.Client.PullRequests.Merge(
			ctx, owner, repo, number, msg,
			&gogithub.PullRequestOptions{CommitTitle: msg},
		)
		if !shouldRetry(err) {
			if err == nil {
				logrus.Infof("Successfully merged commit %d", number)
			}
			m, xerr := ioutil.ReadAll(r.Body)
			if xerr != nil {
				logrus.Error("Could not read failed merge body")
			}
			return errors.Wrap(err, string(m))
		}
	}
}

// GetPullRequest retrieves a PR from GitHub
func (github *GitHub) GetPullRequest(
	ctx context.Context, owner, repo string, prID int,
) (pr *gogithub.PullRequest, err error) {
	pr, _, err = github.client.GetPullRequest(ctx, owner, repo, prID)
	return pr, err
}

func (g *githubClient) GetPullRequest(
	ctx context.Context, owner, repo string, number int,
) (*gogithub.PullRequest, *gogithub.Response, error) {
	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		pr, resp, err := g.Client.PullRequests.Get(ctx, owner, repo, number)
		if !shouldRetry(err) {
			return pr, resp, err
		}
	}
}

// ListPullRequestFiles returns a list of modified files in a pull request
func (github *GitHub) ListPullRequestFiles(ctx context.Context, slug string, pr int,
) (files []*gogithub.CommitFile, err error) {
	owner, repo := ParseSlug(slug)
	if owner == "" || repo == "" {
		return nil, errors.New("invalid repo slug")
	}
	// Build the list options from the GH preference
	opts := &gogithub.ListOptions{
		Page:    0,
		PerPage: github.options.GetItemsPerPage(),
	}
	files, err = github.client.ListPullRequestFiles(ctx, owner, repo, pr, opts)
	if err != nil {
		return nil, errors.Wrap(err, "listing pr files")
	}

	return files, nil

}

// ListPullRequestFiles queries the GH api to get the list of changed files in a PR
func (g *githubClient) ListPullRequestFiles(
	ctx context.Context, owner, repo string, number int, opts *gogithub.ListOptions,
) (files []*gogithub.CommitFile, err error) {
	allFiles := []*gogithub.CommitFile{}
	for {
		files, resp, err := g.Client.PullRequests.ListFiles(ctx, owner, repo, number, opts)
		if err != nil {
			return nil, errors.Wrap(err, "getting modified files")
		}
		allFiles = append(allFiles, files...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	logrus.Infof("GitHub: Found %d files changed in pull request #%d", len(allFiles), number)
	return allFiles, nil
}

func (github *GitHub) ListCheckRunsForRef(
	ctx context.Context, slug, ref string,
) (*gogithub.ListCheckRunsResults, error) {
	owner, repo := ParseSlug(slug)
	if owner == "" || repo == "" {
		return nil, errors.New("invalid repo slug")
	}
	opts := &gogithub.ListCheckRunsOptions{
		ListOptions: gogithub.ListOptions{
			Page:    0,
			PerPage: 100,
		},
	}
	return github.client.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
}

func (g *githubClient) ListCheckRunsForRef(
	ctx context.Context, owner, repo, ref string, opts *gogithub.ListCheckRunsOptions) (*gogithub.ListCheckRunsResults, error) {
	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		runs, _, err := g.Client.Checks.ListCheckRunsForRef(ctx, owner, repo, ref, opts)
		if !shouldRetry(err) {
			return runs, nil
		}
	}
}

// GetIssue retrieves an issue from GitHub
func (github *GitHub) GetIssue(
	ctx context.Context, owner, repo string, IssueID int,
) (pr *gogithub.Issue, err error) {
	pr, _, err = github.client.GetIssue(ctx, owner, repo, IssueID)
	return pr, err
}

func (g *githubClient) GetIssue(
	ctx context.Context, owner, repo string, number int,
) (*gogithub.Issue, *gogithub.Response, error) {
	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		issue, resp, err := g.Client.Issues.Get(ctx, owner, repo, number)
		if !shouldRetry(err) {
			return issue, resp, err
		}
	}
}

// GetIssueComments return the comments of an issue or pr
func (github *GitHub) GetIssueComments(ctx context.Context, slug string, number int,
) (comments []*gogithub.IssueComment, err error) {
	owner, repo := ParseSlug(slug)
	if owner == "" || repo == "" {
		return nil, errors.New("invalid repo slug")
	}
	opts := &gogithub.IssueListCommentsOptions{
		ListOptions: gogithub.ListOptions{
			Page:    0,
			PerPage: github.options.GetItemsPerPage(),
		},
	}

	return github.client.GetIssueComments(ctx, owner, repo, number, opts)
}

// GetIssueComments gets all the comments in a PR from github
func (g *githubClient) GetIssueComments(
	ctx context.Context, owner, repo string, number int, opts *gogithub.IssueListCommentsOptions,
) (comments []*gogithub.IssueComment, err error) {
	// Comment slkice to retrurn
	comments = []*gogithub.IssueComment{}
	// Loop the comment pages
	for {
		for shouldRetry := internal.DefaultGithubErrChecker(); ; {
			cm, resp, err := g.Client.Issues.ListComments(ctx, owner, repo, number, opts)
			if !shouldRetry(err) {
				labels := append(comments, cm...)
				if resp.NextPage == 0 {
					return labels, nil
				}
				opts.Page = resp.NextPage
			}
		}
	}
}

// GetAPIUser returns the authenticated user from the tken
func (github *GitHub) GetAPIUser(ctx context.Context) (user *gogithub.User, err error) {
	return github.client.GetAPIUser(ctx)
}

// GetAPIUser calls the github API to get the current user
func (g *githubClient) GetAPIUser(ctx context.Context) (user *gogithub.User, err error) {
	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		user, _, err := g.Client.Users.Get(ctx, "")
		if !shouldRetry(err) {
			return user, err
		}
	}
}

func (g *githubClient) GetComment(
	ctx context.Context, owner, repo string, number int64,
) (*gogithub.IssueComment, *gogithub.Response, error) {
	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		comment, resp, err := g.Client.Issues.GetComment(ctx, owner, repo, number)
		if !shouldRetry(err) {
			return comment, resp, err
		}
	}
}

// ListLabels return a list of labels
func (g *githubClient) ListLabels(
	ctx context.Context, owner, repo string, gopts *Options,
) ([]*gogithub.Label, error) {
	opts := &gogithub.ListOptions{
		Page:    0,
		PerPage: gopts.GetItemsPerPage(),
	}
	labels := []*gogithub.Label{}
	for {
		for shouldRetry := internal.DefaultGithubErrChecker(); ; {
			loopLabels, resp, err := g.Client.Issues.ListLabels(ctx, owner, repo, opts)
			if !shouldRetry(err) {
				labels := append(labels, loopLabels...)
				if resp.NextPage == 0 {
					return labels, nil
				}
				opts.Page = resp.NextPage
			}
		}
	}
}

func (g *githubClient) AddLabel(
	ctx context.Context, owner, repo string, issue int, label string,
) error {
	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		_, _, err := g.Client.Issues.AddLabelsToIssue(ctx, owner, repo, issue, []string{label})
		if !shouldRetry(err) {
			return err
		}
	}
}

func (g *githubClient) RemoveLabel(
	ctx context.Context, owner, repo string, issue int, label string,
) error {
	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		resp, err := g.Client.Issues.RemoveLabelForIssue(ctx, owner, repo, issue, label)
		// If we get an error, but it is 404 warn but do not err
		if resp.StatusCode == 404 {
			logrus.Warnf("Issue %d does not have label %s, cannot remove (NOOP)", issue, label)
			return nil
		}
		if !shouldRetry(err) {
			return err
		}
	}
}

// ParseSlug splits a repo slug (org/repo) into org + repo strings
func ParseSlug(slug string) (string, string) {
	partes := strings.Split(slug, "/")
	if len(partes) != 2 {
		logrus.Warn("Invalid repo slug")
		return "", ""
	}
	return partes[0], partes[1]
}

// CreatePRComment returns the authenticated user using the GH token
func (github *GitHub) CreateComment(ctx context.Context, slug string, number int, body string,
) (user *gogithub.IssueComment, err error) {
	owner, repo := ParseSlug(slug)
	if owner == "" || repo == "" {
		return nil, errors.New("invalid repo slug")
	}
	return github.client.CreateComment(ctx, owner, repo, number, body)
}

// GetAPIUser calls the github API to get the current user
func (g *githubClient) CreateComment(
	ctx context.Context, owner, repo string, number int, body string,
) (user *gogithub.IssueComment, err error) {

	// comment := &gogithub.PullRequestComment{
	comment := &gogithub.IssueComment{
		Body: &body,
	}

	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		//cm, _, err := g.Client.PullRequests.CreateComment(ctx, owner, repo, number, comment)

		cm, _, err := g.Client.Issues.CreateComment(ctx, owner, repo, number, comment)
		if !shouldRetry(err) {
			return cm, err
		}
	}
}

// CreatePRComment returns the authenticated user using the GH token
func (github *GitHub) DeleteComment(ctx context.Context, slug string, commentID int64,
) (err error) {
	owner, repo := ParseSlug(slug)
	if owner == "" || repo == "" {
		return errors.New("invalid repo slug")
	}
	return github.client.DeleteComment(ctx, owner, repo, commentID)
}

// GetAPIUser calls the github API to get the current user
func (g *githubClient) DeleteComment(
	ctx context.Context, owner, repo string, commentID int64,
) (err error) {

	for shouldRetry := internal.DefaultGithubErrChecker(); ; {
		resp, err := g.Client.Issues.DeleteComment(ctx, owner, repo, commentID)
		if !shouldRetry(err) {
			if err != nil {
				return errors.Wrap(err, "deleting comment")
			}
			if resp.StatusCode != 204 {
				return errors.New("got an http error response deleting comment")
			}
			return nil
		}
	}
}
