package miniprow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gogithub "github.com/google/go-github/v48/github"
	"github.com/sirupsen/logrus"
	"github.com/uservers/miniprow/pkg/github"
	"github.com/uservers/miniprow/pkg/owners"
	"sigs.k8s.io/release-utils/util"
)

const (
	MiniProwDir          = ".miniprow"
	MiniProwConf         = "config.yaml"
	approvalNotifierFlag = "APPROVALNOTIFIER"
	TestsDoneCommand     = "tests-done"
)

type Broker struct {
	ctx    context.Context
	impl   brokerImplementation
	github *github.GitHub
	config Config
	State  *State
}

type State struct {
	PullRequest *gogithub.PullRequest
	Issue       *gogithub.Issue
}

var DefaultConfig = Config{
	requiredLabels: []string{"approved", "lgtm"},
	options: &Options{
		AutoMerge: true, // AutoMerge merges a PR if the author is an approver + reviewer
	},
}

type Options struct {
	AutoMerge bool
}

type Config struct {
	requiredLabels []string
	options        *Options
}

// RequiredLabels returns a list of required labels
func (c *Config) RequiredLabels() []string {
	return c.requiredLabels
}

func NewBroker() (*Broker, error) {
	broker := &Broker{
		impl:   &defaultBrokerImplementation{},
		config: DefaultConfig,
	}

	// Load the context data from the environment
	broker.ReadContext()

	// Load configuration file
	if err := broker.LoadConfigFile(); err != nil {
		return nil, fmt.Errorf("loading config file: %w", err)
	}

	// Load the state
	if err := broker.InitState(); err != nil {
		return nil, fmt.Errorf("initilizing state: %w", err)
	}

	return broker, nil
}

// Run starts the processing
func (b *Broker) Run() (err error) {
	logrus.WithField("step", "Run").Info("🚀 MiniProw broker running!")
	switch b.ctx.Value(ckey).(ContextData).Event() {
	case "COMMENT":
		if err := b.HandleComment(); err != nil {
			logrus.WithField("step", "Run").Error(err)
			return fmt.Errorf("running comment handler: %w", err)
		}
	case "NEWPR":
		if err := b.HandleNewPR(); err != nil {
			logrus.WithField("step", "Run").Error(err)
			return fmt.Errorf("running new PR handler: %w", err)
		}
	case "CHECKMERGE":
		if err := b.CheckMerge(); err != nil {
			logrus.WithField("step", "Run").Error(err)
			return fmt.Errorf("running merge check: %w", err)
		}
	case "TESTSDONE":
		if err := b.CreateTestsDoneComment(); err != nil {
			logrus.WithField("step", "Run").Error(err)
			return fmt.Errorf("running merge check: %w", err)
		}
	default:
		logrus.WithField("step", "Run").Error("MiniProw event not found or wrong key")
		return errors.New("unkown MiniProw event")
	}

	// TODO(puerco): Check if pr is merged before continuing
	//nolint:gocritic
	/*
		// Checking if PR can be merged
		ready, err := b.LabelsReadyToMerge()
		if err != nil {
			return fmt.Errorf( "checking if PR is ready: %w", err)
		}

		if !ready {
			logrus.WithField("step", "Run").Info("PR cannot be merged yet")
			return nil
		}

		// Verify the test suite has been successful
		checksPassed, err := b.VerifyChecks()
		if err != nil {
			return fmt.Errorf( "verifying check suite results from the PR: %w", err)
		}

		if !checksPassed {
			logrus.WithField("step", "Run").Info("⏳ PR checks are failing or have not yet completed")
			return nil
		}

		// Merge PR
		if err := b.MergePullRequest(); err != nil {
			return fmt.Errorf("merging pull request: %w", err)
		}
	*/
	return nil
}

// GitHub returns a github object
func (b *Broker) GitHub() *github.GitHub {
	if b.github == nil {
		gh, err := b.impl.GetGitHub(b.ctx)
		if err == nil {
			b.github = gh
		} else {
			logrus.Error(fmt.Errorf("creating github object: %w", err))
		}
	}
	return b.github
}

// RepoRoot returns the path to the repository root
func (b *Broker) RepoRoot() string {
	return b.impl.RepoRoot(b.ctx)
}

// LoadConfigFile reads the borker configuration from a file
func (b *Broker) LoadConfigFile() error {
	return b.impl.LoadConfigFile(b.ctx)
}

//counterfeiter:generate . brokerImplementation
type brokerImplementation interface {
	ReadContext() context.Context
	ReadState(ctx context.Context) (*State, error)
	GetGitHub(context.Context) (*github.GitHub, error)
	GetComment(*github.GitHub, string, int64) (*gogithub.IssueComment, error)
	GetPullRequest(*github.GitHub, string, int) (*gogithub.PullRequest, error)
	GetIssue(*github.GitHub, string, int) (*gogithub.Issue, error)
	MergePullRequest(context.Context, *github.GitHub, string, int) error
	GetRepoOwners(context.Context) (*owners.List, error)
	AddLabel(context.Context, *github.GitHub, string) error
	GetChangedFiles(context.Context, *github.GitHub) ([]*gogithub.CommitFile, error)
	RepoRoot(context.Context) string
	LoadConfigFile(context.Context) error
	GetApprovers(context.Context, *github.GitHub) ([]string, []string, error)
	GetMissingApprovers(context.Context, *github.GitHub, []string) ([]*fileApprovers, error)
	GetNeededApprovers(context.Context, *github.GitHub) (*owners.List, error)
	GetUserPerms(context.Context, string) (map[string]bool, error)
	GetAuthor(s *State) string
	GetPRCheckRuns(context.Context, *github.GitHub, *State) (*gogithub.ListCheckRunsResults, error)
	GetApprovalNotifierComment(context.Context, *github.GitHub, *State) (*gogithub.IssueComment, error)
	GetBotUser(context.Context, *github.GitHub) (*gogithub.User, error)
	IsApprovalNotifier(*gogithub.IssueComment, string) bool
	CreatePRComment(context.Context, *github.GitHub, *State, string) (*gogithub.IssueComment, error)
	DeletePRComment(context.Context, *github.GitHub, int64) error
}

type defaultBrokerImplementation struct{}

// GetGitHub gets a comment
func (bi *defaultBrokerImplementation) GetGitHub(ctx context.Context) (*github.GitHub, error) {
	if ctx.Value(ckey).(ContextData).GitHubToken() == "" {
		return nil, errors.New("unable to get github client, token not found")
	}
	gh, err := github.NewWithToken(ctx.Value(ckey).(ContextData).GitHubToken())
	if err != nil {
		return nil, fmt.Errorf("creating github object: %w", err)
	}
	return gh, nil
}

// GetComment return a comment object from its id
func (bi *defaultBrokerImplementation) GetComment(
	gh *github.GitHub, slug string, commentID int64,
) (comment *gogithub.IssueComment, err error) {
	// Parse the slug
	org, repo := github.ParseSlug(slug)
	if org == "" || repo == "" {
		return nil, errors.New("unable to get comment, repo slug not valid")
	}
	// Call the github api to get the comment
	comment, err = gh.GetComment(org, repo, commentID)
	if err != nil {
		return comment, fmt.Errorf("getting comment from github: %w", err)
	}
	return comment, nil
}

// GetComment return a comment object from its id
func (bi *defaultBrokerImplementation) GetPullRequest(
	gh *github.GitHub, slug string, prID int,
) (pr *gogithub.PullRequest, err error) {
	// Parse the slug
	org, repo := github.ParseSlug(slug)
	if org == "" || repo == "" {
		return nil, errors.New("unable to get comment, repo slug not valid")
	}
	// Call the github api to get the comment
	pr, err = gh.GetPullRequest(context.Background(), org, repo, prID)
	if err != nil {
		return pr, fmt.Errorf("getting comment from github: %w", err)
	}
	return pr, nil
}

// GetComment return a comment object from its id
func (bi *defaultBrokerImplementation) GetIssue(
	gh *github.GitHub, slug string, issueID int,
) (pr *gogithub.Issue, err error) {
	// Parse the slug
	org, repo := github.ParseSlug(slug)
	if org == "" || repo == "" {
		return nil, errors.New("unable to get comment, repo slug not valid")
	}
	// Call the github api to get the comment
	pr, err = gh.GetIssue(context.Background(), org, repo, issueID)
	if err != nil {
		return pr, fmt.Errorf("getting comment from github: %w", err)
	}
	return pr, nil
}

// ReadContext builds the context from the environment data
func (bi *defaultBrokerImplementation) ReadContext() context.Context {
	// Build the context we will use
	return context.WithValue(context.Background(), ckey, NewContextData())
}

// ReadState reads the state and buids the object
func (bi *defaultBrokerImplementation) ReadState(ctx context.Context) (s *State, err error) {
	s = &State{}
	gh, err := bi.GetGitHub(ctx)
	if err != nil {
		return s, fmt.Errorf("creating github client: %w", err)
	}

	// Check if we are dealing with an issue and assign to state
	if issueID := ctx.Value(ckey).(ContextData).Issue(); issueID != 0 {
		issue, err := bi.GetIssue(gh, ctx.Value(ckey).(ContextData).Repository(), issueID)
		if err != nil {
			return nil, fmt.Errorf("reading issue to assign in state: %w", err)
		}
		logrus.WithContext(ctx).WithField("step", "ReadState").Infof(
			"Got Issue #%d from context", issue.GetNumber(),
		)
		s.Issue = issue
	}

	// or if we are dealing with a PR
	if prID := ctx.Value(ckey).(ContextData).PullRequest(); prID != 0 {
		pr, err := bi.GetPullRequest(gh, ctx.Value(ckey).(ContextData).Repository(), prID)
		if err != nil {
			return nil, fmt.Errorf("fetching PR #%d: %w", prID, err)
		}
		logrus.WithContext(ctx).WithField("step", "ReadState").Infof(
			"Got Pull Request #%d from context", pr.GetNumber(),
		)
		s.PullRequest = pr
	}

	return s, nil
}

// Returns the author of the PR or issue
func (b *Broker) Author() string {
	return b.impl.GetAuthor(b.State)
}

// HandleNewPR runs whena new PR is created
func (b *Broker) HandleNewPR() error {
	if b.ctx.Value(ckey).(ContextData).PullRequest() == 0 {
		return errors.New("cannot start new PR handler, PR number not set")
	}
	logrus.Infof(
		"New Pull Request handler running for PR ID#%d",
		b.ctx.Value(ckey).(ContextData).PullRequest(),
	)

	// Get the PR author login
	author := b.Author()
	if author == "" {
		return errors.New("unable to handle pr, could not get PR author handle")
	}

	// Get the current user top-level permissions
	userPerms, err := b.impl.GetUserPerms(b.ctx, author)
	if err != nil {
		return fmt.Errorf("getting the PR author's permissions: %w", err)
	}

	// list of approvals needed:
	neededApproves := owners.NewList()

	// If the user is a top-level approver, we skip the
	// file permission check
	if userPerms["approver"] {
		logrus.Info("Not checking individual files as user is a top level approver")
	} else {
		// Otherwise, we get the missing approvers. We need to check each file

		// First, get the list of current approvers
		neededApproves, err = b.impl.GetNeededApprovers(b.ctx, b.GitHub())
		if err != nil {
			return fmt.Errorf("getting current PR approvers: %w", err)
		}
	}

	// if the user is an approver, we always add the label
	// TODO(puerco): Mal, implementa
	if userPerms["approver"] || len(neededApproves.Files) == 0 {
		logrus.Infof("→ %s is an aprrover", author)
		if err := b.impl.AddLabel(b.ctx, b.GitHub(), "approved"); err != nil {
			return fmt.Errorf("adding approved label: %w", err)
		}
	}

	// Now, if the user is a reviewer...
	if userPerms["reviewer"] {
		logrus.Infof("→ %s is a reviewer", author)
		// ... and also an approver *and* we have automerge on
		if userPerms["approver"] && b.config.options.AutoMerge {
			if err := b.impl.AddLabel(b.ctx, b.GitHub(), "lgtm"); err != nil {
				return fmt.Errorf("adding approved lgtm: %w", err)
			}
		}
	}

	if !userPerms["reviewer"] && !userPerms["approver"] {
		logrus.Infof("User %s is not an approver nor a reviewer", author)
	}

	if err := b.CreateApprovalNotifierComment(); err != nil {
		return fmt.Errorf("creating approval notifier: %w", err)
	}
	return nil
}

func (b *Broker) CheckMerge() error {
	// 1, Check the comment and the check runs to se if we need to
	// add the comment again
	checksReady, err := b.ChecksReadyToMerge()
	if err != nil {
		return fmt.Errorf("looking at pull request check runs: %w", err)
	}

	// 2. Check the labels are ready
	labelsReady, err := b.LabelsReadyToMerge()
	if err != nil {
		return fmt.Errorf("looking at pull request labels: %w", err)
	}

	// We need the labels and the checks to merge
	if !labelsReady && !checksReady {
		missing := []string{}
		if !labelsReady {
			missing = append(missing, "labels")
		}
		if !checksReady {
			missing = append(missing, "checks")
		}
		logrus.Infof("Pull request not yet ready to merge. Has missing: %s", strings.Join(missing, ","))
	}

	// Merge PR → → → →
	if err := b.MergePullRequest(); err != nil {
		return fmt.Errorf("merging pull request: %w", err)
	}
	return nil
}

// Handles a new comment posted
func (b *Broker) HandleComment() error {
	logrus.Infof(
		"💬 Comment handler running for comment ID#%d",
		b.ctx.Value(ckey).(ContextData).CommentID(),
	)
	gh, err := b.impl.GetGitHub(b.ctx)
	if err != nil {
		return fmt.Errorf("creating github object: %w", err)
	}

	// Get the comment
	comment, err := b.impl.GetComment(
		gh, b.ctx.Value(ckey).(ContextData).Repository(),
		b.ctx.Value(ckey).(ContextData).CommentID(),
	)
	if err != nil {
		return fmt.Errorf("getting comment from github: %w", err)
	}

	// The comment is the APPROVALNOTIFIER comment, handle it now
	// as we know this comment does not have any flags.
	//
	// To determine if its the real deal, we look for the flag *and*
	// check it is comming from the bot account:
	botuser, err := b.impl.GetBotUser(b.ctx, b.GitHub())
	if err != nil {
		return fmt.Errorf("getting bot user: %w", err)
	}
	if b.impl.IsApprovalNotifier(comment, botuser.GetLogin()) {
		logrus.Info(" > Event triggered by Approval Notifier Comment")
		return b.HandleApprovalNotifierComment(comment)
	}

	logrus.Info(" > Performing comment analysis")

	// Extract the slash commands from the comment body
	logrus.WithField("handler", "comment handler").Infof(
		" > Comment body: %s", strings.TrimSpace(comment.GetBody()),
	)
	commands, err := ParseSlashCommands(strings.TrimSpace(comment.GetBody()))
	if err != nil {
		return fmt.Errorf("parsing commands: %w", err)
	}

	logrus.WithField("handler", "comment handler").Infof(
		"Found %d slash commands in the comment data", len(commands),
	)
	var combinedError error

	// Cycle all commands found and run them
	for _, cmd := range commands {
		if err := cmd.Run(b); err != nil {
			if combinedError == nil {
				combinedError = errors.New("errors while running handlers")
			}
			combinedError = fmt.Errorf(combinedError.Error(), err)
		}
	}

	if combinedError != nil {
		return combinedError
	}

	// Check if we can merge after the slash commands
	if err := b.CreateApprovalNotifierComment(); err != nil {
		return fmt.Errorf("creating ANC after slash commands: %w", err)
	}
	return nil
}

// ChecksReadyToMerge returns true if the checks in a PR are ready to merge
func (b *Broker) ChecksReadyToMerge() (ready bool, err error) {
	logrus.Info("🔍 Verifying checks have completed and are successful")
	// Get the latest runs in the pull request
	checkruns, err := b.impl.GetPRCheckRuns(b.ctx, b.GitHub(), b.State)
	if err != nil {
		return ready, fmt.Errorf("getting check runs for pull request: %w", err)
	}

	failedJobs := []*gogithub.CheckRun{}
	for _, run := range checkruns.CheckRuns {
		// If we have at least one check still running, we do not
		// continue checking as we are sure we cannot merge
		if run.GetStatus() != "completed" {
			logrus.Infof(" > check %s has not yet completed", run.GetName())
			return false, nil
		}

		// Count the jobs that are failing
		if run.GetConclusion() != "success" {
			logrus.Infof(" > last run of %s failed", run.GetName())
			failedJobs = append(failedJobs, run)
		}
	}

	// If we have failed jobs, we cannot merge just now
	if len(failedJobs) > 0 {
		logrus.Infof("❌ %d jobs are failing. Cannot merge just now.", len(failedJobs))
		return false, nil
	}

	logrus.Info(" ✅ CI Tests are green")
	return true, nil
}

// CreateTestsDoneComment creates a comment to notify that tests are done
func (b *Broker) CreateTestsDoneComment() error {
	if _, err := b.impl.CreatePRComment(b.ctx, b.GitHub(), b.State, "/"+TestsDoneCommand); err != nil {
		return fmt.Errorf("creating approval notifier comment: %w", err)
	}
	return nil
}

// CreateApprovalNotifierComment posts a new comment in the PullRequest
// witht he special flag to handle the merging of the PRs
func (b *Broker) CreateApprovalNotifierComment() error {
	// First, check if the comment exists already and delete it
	comment, err := b.impl.GetApprovalNotifierComment(b.ctx, b.GitHub(), b.State)
	if err != nil {
		return fmt.Errorf("while lookig for the approve notifier comment: %w", err)
	}

	repoRoot := b.RepoRoot()
	if repoRoot == "" {
		return fmt.Errorf("could not get repo root: %w", err)
	}

	// Get the list of needed approvers
	neededApprovers, err := b.impl.GetNeededApprovers(b.ctx, b.GitHub())
	if err != nil {
		return fmt.Errorf("getting current PR approvers: %w", err)
	}

	if len(neededApprovers.Approvers) == 0 {
		return errors.New("no approvers were found. Missing OWNERS file(s)?")
	}

	// Get the approvers and reviewers
	approvers, _, err := b.impl.GetApprovers(b.ctx, b.GitHub())
	if err != nil {
		return fmt.Errorf("while getting current PR approvers: %w", err)
	}
	approvers = append(approvers, b.impl.GetAuthor(b.State))

	// TODO(puerco): Implement suggestions here
	suggestedAssignees := []string{}

	// If the comment exists already delete it
	if comment != nil {
		if err := b.impl.DeletePRComment(b.ctx, b.GitHub(), comment.GetID()); err != nil {
			return fmt.Errorf("deleting previous approval notifier: %w", err)
		}
	}

	commentBody := "[" + approvalNotifierFlag + "] This PR is __NOT APPROVED__\n\n\n"
	commentBody += "This pull-request has been approved by: *" + strings.Join(approvers, ", ") + "*\n"
	commentBody += "To complete the pull request process, please assign " + strings.Join(suggestedAssignees, ",")
	commentBody += " after the PR has been reviewed.\n"
	commentBody += "You can assign the PR to them by writing `/assign "
	commentBody += strings.Join(suggestedAssignees, ",") + "` in a comment when ready.\n\n"

	commentBody += "The full list of commands accepted by this bot can be found [here](http://undercons.com/).\n\n"
	// TODO: Check if all are approved and do not open details
	commentBody += "<details open>\n"
	commentBody += "Needs approval from an approver in each of these files:\n\n"
	for _, ofile := range neededApprovers.Files {
		fileApprovers := ofile.WhoCanApprove(approvers)
		mkup := "**"
		if len(fileApprovers) > 0 {
			mkup = "~~"
		}

		commentBody += fmt.Sprintf(
			"- %s[%s](%s)%s", mkup,
			strings.TrimPrefix(ofile.Path, repoRoot),
			"http://github.com/", mkup,
		)

		if len(fileApprovers) > 0 {
			commentBody += " [" + strings.Join(fileApprovers, ",") + "]"
		}

		commentBody += "\n"
	}

	commentBody += "\n"

	commentBody += "Approvers can indicate their approval by writing /approve in a comment\n\n"
	commentBody += "Approvers can cancel approval by writing /approve cancel in a comment\n"
	commentBody += "</details>"
	// Post the new comment to the pull request
	_, err = b.impl.CreatePRComment(b.ctx, b.GitHub(), b.State, commentBody)
	if err != nil {
		return fmt.Errorf("creating approval notifier comment: %w", err)
	}

	return nil
}

// HandleApprovalNotifierComment reacts on the approval comment
// ti check if tests are ready and flags are in place
func (b *Broker) HandleApprovalNotifierComment(comment *gogithub.IssueComment) error {
	// 1, Check the comment and the check runs to se if we need to
	// add the comment again
	checksReady, err := b.ChecksReadyToMerge()
	if err != nil {
		return fmt.Errorf("looking at pull request check runs: %w", err)
	}

	// 2. Check the labels are ready
	labelsReady, err := b.LabelsReadyToMerge()
	if err != nil {
		return fmt.Errorf("looking at pull request labels: %w", err)
	}

	// We need the labels and the checks to merge
	if !labelsReady || !checksReady {
		missing := []string{}
		if !labelsReady {
			missing = append(missing, "labels")
		}
		if !checksReady {
			missing = append(missing, "checks")
		}
		logrus.Infof(
			"⏳ Not merging as pull request is not yet ready. Has missing: %s",
			strings.Join(missing, ","),
		)
		return nil
	}

	// Merge PR → → → →
	if err := b.MergePullRequest(); err != nil {
		return fmt.Errorf("merging pull request: %w", err)
	}

	// If PR has merged, delete the approval notifier
	if err := b.impl.DeletePRComment(b.ctx, b.GitHub(), comment.GetID()); err != nil {
		logrus.Error("error deleting the approval notifier comment")
	}

	return nil
}

// VerifyChecks checks if all tests are green
func (b *Broker) VerifyChecks() (checksPassed bool, err error) {
	// Log
	logrus.Infof("🔍 Verifying checks for pull request %d", b.State.PullRequest.GetNumber())
	// Get the checks from the sha point
	checkruns, err := b.impl.GetPRCheckRuns(b.ctx, b.GitHub(), b.State)
	if err != nil {
		return false, fmt.Errorf("getting check runs for pull request: %w", err)
	}
	failed := 0
	for _, run := range checkruns.CheckRuns {
		if run.GetStatus() == "completed" {
			if run.GetConclusion() == "success" {
				logrus.Infof(" ✅ %s passed", run.GetName())
			} else {
				logrus.Infof(" ❌ last run of %s failed", run.GetName())
				failed++
			}
		} else {
			logrus.Warnf(" ⏳ check run %s has not completed yet", run.GetName())
			failed++
		}
	}
	if failed == 0 {
		return true, nil
	}
	return false, nil
}

// GetPRCheckRuns returns the check runs in a git ref ref
func (bi *defaultBrokerImplementation) GetPRCheckRuns(
	ctx context.Context, gh *github.GitHub, state *State,
) (*gogithub.ListCheckRunsResults, error) {
	if state.PullRequest == nil {
		return nil, errors.New("no pr found in state")
	}
	runs, err := gh.ListCheckRunsForRef(
		ctx, ctx.Value(ckey).(ContextData).Repository(),
		state.PullRequest.GetHead().GetSHA(),
	)
	if err != nil {
		return nil, fmt.Errorf(
			"getting check runs for PR %d: %w",
			state.PullRequest.GetNumber(), err,
		)
	}
	return runs, nil
}

// LabelsReadyToMerge checks if the PR is ready to merge
func (b *Broker) LabelsReadyToMerge() (ready bool, err error) {
	logrus.Infof("🔍 checking if PR %d can merge", b.ctx.Value(ckey).(ContextData).PullRequest())
	// Fetch the pull request from GitHub
	pr, err := b.impl.GetPullRequest(
		b.GitHub(),
		b.ctx.Value(ckey).(ContextData).Repository(),
		b.ctx.Value(ckey).(ContextData).PullRequest(),
	)
	if err != nil {
		return ready, fmt.Errorf("fetching PR from GitHub: %w", err)
	}

	// Check that the required labels are set
	missingLabels := []string{}
	logrus.Infof("PR requires the following labels: %s", strings.Join(b.config.RequiredLabels(), ", "))

RequiredLoop:
	for _, expected := range b.config.requiredLabels {
		for _, l := range pr.Labels {
			if expected == l.GetName() {
				logrus.Infof(" > found label %s", l.GetName())
				continue RequiredLoop
			}
		}
		missingLabels = append(missingLabels, expected)
	}
	if len(missingLabels) > 0 {
		errMsg := fmt.Sprintf("cannot merge PR #%d, has missing labels: %s",
			pr.GetNumber(), strings.Join(missingLabels, ", "))
		logrus.Info("❌ " + errMsg)
		return false, errors.New(errMsg)
	}

	if pr.GetMerged() {
		errMsg := fmt.Sprintf("PR #%d, is already merged", pr.GetNumber())
		logrus.Info("❌ " + errMsg)
		return false, errors.New(errMsg)
	}

	if !pr.GetMergeable() {
		errMsg := fmt.Sprintf("github reports that PR #%d, cannot merge yet", pr.GetNumber())
		logrus.Info("❌ " + errMsg)
		return false, errors.New(errMsg)
	}

	logrus.Infof("✅ Pull Request #%d has all labels required to merge", pr.GetNumber())
	return true, nil
}

// ReadContext reads the environment and assigns the data to the context
func (b *Broker) ReadContext() {
	b.ctx = b.impl.ReadContext()
}

func (b *Broker) InitState() error {
	s, err := b.impl.ReadState(b.ctx)
	if err != nil {
		return fmt.Errorf("initializing state: %w", err)
	}
	b.State = s
	return nil
}

// MergePullRequest merges the pull request
func (b *Broker) MergePullRequest() error {
	logrus.Infof("🏁 merging Pull Request #%d", b.ctx.Value(ckey).(ContextData).PullRequest())
	// Fetch the pull request from GitHub
	pr, err := b.impl.GetPullRequest(
		b.GitHub(),
		b.ctx.Value(ckey).(ContextData).Repository(),
		b.ctx.Value(ckey).(ContextData).PullRequest(),
	)
	if err != nil {
		return fmt.Errorf("fetching PR from GitHub: %w", err)
	}

	if pr.GetMerged() {
		logrus.Warnf("PR %d is already merged! (NOOP)", pr.GetNumber())
		return nil
	}

	if err := b.impl.MergePullRequest(
		b.ctx, b.GitHub(), b.ctx.Value(ckey).(ContextData).Repository(), pr.GetNumber(),
	); err != nil {
		return fmt.Errorf("merging pull request: %w", err)
	}
	return nil
}

// MergePullRequest calls the GH API to merge the PR
func (bi *defaultBrokerImplementation) MergePullRequest(
	ctx context.Context, gh *github.GitHub, repoSlug string, prID int,
) error {
	org, repo := github.ParseSlug(repoSlug)
	if org == "" || repo == "" {
		return errors.New("unable to get comment, repo slug not valid")
	}
	return gh.MergePullRequest(ctx, org, repo, prID)
}

// GetRepoOwners gets the owners from the top OWNERS file
func (bi *defaultBrokerImplementation) GetRepoOwners(
	ctx context.Context,
) (list *owners.List, err error) {
	repoRoot := bi.RepoRoot(ctx)
	if repoRoot == "" {
		return list, errors.New("unable to load config, reporoot not found")
	}
	// There must be a better way to find the cloned repo
	reader := owners.NewReader()
	list, err = reader.GetDirectoryOwners(repoRoot)
	if err != nil {
		return list, fmt.Errorf("reading top repository OWNERS file: %w", err)
	}
	return list, nil
}

// Adds a label to the current issue/PR
func (bi *defaultBrokerImplementation) AddLabel(
	ctx context.Context, gh *github.GitHub, labelName string,
) error {
	org, repo := github.ParseSlug(ctx.Value(ckey).(ContextData).Repository())
	if org == "" || repo == "" {
		return errors.New("unable to get comment, repo slug not valid")
	}
	issueID := ctx.Value(ckey).(ContextData).PullRequest()
	if issueID == 0 {
		ctx.Value(ckey).(ContextData).Issue()
	}
	if issueID == 0 {
		return errors.New("unable to add label, cannot find issue or pr number in context")
	}
	logrus.Infof("Adding label %s to issue #%d", labelName, issueID)
	if err := gh.AddLabel(org, repo, issueID, labelName); err != nil {
		return fmt.Errorf("adding label to #%d: %w", issueID, err)
	}
	return nil
}

// GetChangedFiles returns a list of the changed files in the current PR
func (bi *defaultBrokerImplementation) GetChangedFiles(ctx context.Context, gh *github.GitHub,
) (files []*gogithub.CommitFile, err error) {
	// Get the modified files
	files, err = gh.ListPullRequestFiles(ctx,
		ctx.Value(ckey).(ContextData).Repository(),
		ctx.Value(ckey).(ContextData).PullRequest(),
	)
	if err != nil {
		return files, fmt.Errorf(
			"listing PR#%d files: %w", ctx.Value(ckey).(ContextData).PullRequest(), err,
		)
	}
	logrus.Infof(
		"Found %d modified files in PR#%d", len(files),
		ctx.Value(ckey).(ContextData).PullRequest(),
	)
	return files, nil
}

func (bi *defaultBrokerImplementation) RepoRoot(ctx context.Context) string {
	org, repo := github.ParseSlug(ctx.Value(ckey).(ContextData).Repository())
	if org == "" || repo == "" {
		logrus.Error("Unable to infer repository root, repo slug not valid")
	}
	// There must be a better way to find the cloned repo
	root := os.Getenv("GITHUB_WORKSPACE")
	if !util.Exists(root) {
		logrus.Errorf("Unable to find repository root in %s", root)
		return ""
	}
	return root
}

// LoadConfigFile loads a conf file from the miniprow directory
func (bi *defaultBrokerImplementation) LoadConfigFile(ctx context.Context) error {
	repoRoot := bi.RepoRoot(ctx)
	if repoRoot == "" {
		return errors.New("unable to load config, repo root not found")
	}
	confpath := filepath.Join(repoRoot, MiniProwDir, MiniProwDir)
	logrus.Warn("Config file load not yet implemented")
	if util.Exists(confpath) {
		logrus.Info("Loading configuration file from " + confpath)
	} else {
		logrus.Warn("No configuration file found. Using default values")
	}
	return nil
}

// fileApprovers is a type that binds a filename and its owners
type fileApprovers struct {
	Filename string
	Owners   owners.List
}

func (bi *defaultBrokerImplementation) GetMissingApprovers(
	ctx context.Context, gh *github.GitHub, currentApprovers []string,
) (approvals []*fileApprovers, err error) {
	// Check the files modified by the PR
	files, err := bi.GetChangedFiles(ctx, gh)
	if err != nil {
		return approvals, fmt.Errorf("listing pull request files: %w", err)
	}

	// Build a revers lookup map
	revkey := map[string]struct{}{}
	for _, user := range currentApprovers {
		revkey[user] = struct{}{}
	}

	approvals = []*fileApprovers{}
	repoRoot := bi.RepoRoot(ctx)
	if repoRoot == "" {
		return nil, errors.New("unable to load missing approvers, reporoot not found")
	}

	reader := owners.NewReader()

	// Range the files in the PR and get the owners
	for _, f := range files {
		logrus.Infof(" > Checking File: %s", f.GetFilename())

		ownerList, err := reader.GetPathOwners(
			filepath.Join(repoRoot, f.GetFilename()),
		)
		if err != nil {
			return approvals, fmt.Errorf("getting owners from %s: %w", f, err)
		}

		// Check the approvers to see if we have one
		approved := false
		for _, user := range ownerList.Approvers {
			if _, ok := revkey[string(user)]; ok {
				approved = false
				break
			}
		}

		// If no approver was found, add to the list
		if !approved {
			approvals = append(approvals, &fileApprovers{
				Filename: f.GetFilename(),
				Owners:   *ownerList,
			})
		}
	}
	return approvals, nil
}

// GetNeededApprovers  returns the approvals needed to merge the PR
func (bi *defaultBrokerImplementation) GetNeededApprovers(
	ctx context.Context, gh *github.GitHub,
) (list *owners.List, err error) {
	// Check the repository root before proceeding
	repoRoot := bi.RepoRoot(ctx)
	if repoRoot == "" {
		return nil, fmt.Errorf("unable to load missing approvers, reporoot not found")
	}
	// Get the owners list for every file and append them
	files, err := bi.GetChangedFiles(ctx, gh)
	if err != nil {
		return nil, fmt.Errorf("listing pull request files: %w", err)
	}

	// Build the pnwers reader
	reader := owners.NewReader()
	list = owners.NewList()
	for _, file := range files {
		logrus.Infof("📂 Getting approvers for %s", file.GetFilename())
		loopList, err := reader.GetPathOwners(
			filepath.Join(repoRoot, file.GetFilename()),
		)
		if err != nil {
			return nil, fmt.Errorf(
				"getting owners for path: %s: %w", file.GetFilename(), err,
			)
		}
		list.Append(loopList)
	}
	return list, nil
}

func (bi *defaultBrokerImplementation) GetAuthor(s *State) string {
	return s.PullRequest.GetUser().GetLogin()
}

// GetAuthorPerms returs the top-level authorizations of the PR author
func (bi *defaultBrokerImplementation) GetUserPerms(
	ctx context.Context, userName string,
) (userPerms map[string]bool, err error) {
	// Add the automatic labels according to the collaborator types
	userPerms = map[string]bool{
		"approver": false,
		"reviewer": false,
	}

	// Get the top level owners
	ownerList, err := bi.GetRepoOwners(ctx)
	if err != nil {
		return userPerms, fmt.Errorf("getting repository owners: %w", err)
	}

	for _, user := range ownerList.Approvers {
		if userName == string(user) {
			logrus.Infof(
				"User %s is an approver in %s", userName,
				ctx.Value(ckey).(ContextData).Repository(),
			)
			userPerms["approver"] = true
		}
	}
	for _, user := range ownerList.Reviewers {
		if userName == string(user) {
			logrus.Infof(
				"User %s is a reviewer in %s", userName,
				ctx.Value(ckey).(ContextData).Repository(),
			)
			userPerms["reviewer"] = true
		}
	}

	logrus.Infof(
		"Found %d top-level approvers and %d reviewers",
		len(ownerList.Approvers), len(ownerList.Reviewers),
	)
	return userPerms, nil
}

// GetApprovalNotifierComment
func (bi *defaultBrokerImplementation) GetApprovalNotifierComment(
	ctx context.Context, gh *github.GitHub, s *State,
) (comment *gogithub.IssueComment, err error) {
	// Check if we have th PR ready
	if s.PullRequest == nil {
		return comment, errors.New("pull request not found in state")
	}

	// Get tje PR number
	prid := s.PullRequest.GetNumber()
	if prid == 0 {
		return nil, errors.New("unable to determine the PR number")
	}

	// Get all the comments from the PR
	comments, err := gh.GetIssueComments(
		ctx, ctx.Value(ckey).(ContextData).Repository(), prid,
	)
	if err != nil {
		return nil, fmt.Errorf("listing comments to find approval notifier: %w", err)
	}

	// Get the bot user from the GH API
	botuser, err := bi.GetBotUser(ctx, gh)
	if err != nil {
		return nil, fmt.Errorf("getting GitHub api user: %w", err)
	}

	// Range all PRs until we find it
	for _, c := range comments {
		if bi.IsApprovalNotifier(c, botuser.GetLogin()) {
			return c, nil
		}
	}
	logrus.Info("Approval notifier comment not found")
	return nil, nil
}

// IsApprovalNotifier checks a comment to see if it is the approval
// notifier comment. It has to have the flag and the author has to be
// the bot account running miniprow
func (bi *defaultBrokerImplementation) IsApprovalNotifier(
	comment *gogithub.IssueComment, botuser string,
) bool {
	if strings.Contains(comment.GetBody(), "["+approvalNotifierFlag+"]") &&
		comment.GetUser().GetLogin() == botuser {
		return true
	}

	return false
}

func (bi *defaultBrokerImplementation) GetBotUser(
	ctx context.Context, gh *github.GitHub,
) (user *gogithub.User, err error) {
	return gh.GetAPIUser(ctx)
}

// CreatePRComment creates a comment in the pull request
func (bi *defaultBrokerImplementation) CreatePRComment(
	ctx context.Context, gh *github.GitHub, s *State, body string,
) (comment *gogithub.IssueComment, err error) {
	// Get tje PR number
	prid := s.PullRequest.GetNumber()
	if prid == 0 {
		return nil, errors.New("unable to determine the PR number")
	}

	return gh.CreateComment(
		ctx, ctx.Value(ckey).(ContextData).Repository(), prid, body,
	)
}

func (bi *defaultBrokerImplementation) DeletePRComment(
	ctx context.Context, gh *github.GitHub, commentID int64,
) error {
	return gh.DeleteComment(ctx, ctx.Value(ckey).(ContextData).Repository(), commentID)
}

// GetApprovers returns a list of users that have approved this PR
// note the PR author IS NOT INCLUDED IN THIS LIST.
func (bi *defaultBrokerImplementation) GetApprovers(
	ctx context.Context, gh *github.GitHub,
) (approvers, reviewers []string, err error) {
	logrus.Info("🤓 Looking for approvers and reviewers in PR comments")
	// Get all the PR comments
	comments, err := gh.GetIssueComments(
		ctx, ctx.Value(ckey).(ContextData).Repository(),
		ctx.Value(ckey).(ContextData).PullRequest(),
	)
	if err != nil {
		return approvers, reviewers, fmt.Errorf("listing PR comments: %w", err)
	}

	approvers = []string{}
	reviewers = []string{}

	// Cycle all comments and check if they have lgtm'd or approved
	for _, comment := range comments {
		lines := strings.Split(comment.GetBody(), "\n")
		for _, line := range lines {
			tokens := strings.Fields(line)

			// If no words,ignore line
			if len(tokens) == 0 {
				continue
			}

			if tokens[0] == "/approve" {
				approvers = append(approvers, comment.GetUser().GetLogin())
			}

			if tokens[0] == "/lgtm" {
				reviewers = append(reviewers, comment.GetUser().GetLogin())
			}
		}
	}
	logrus.Infof("> Found %d PR approvers: %s", len(approvers), strings.Join(approvers, ", "))
	logrus.Infof("> Found %d PR reviewers: %s", len(reviewers), strings.Join(reviewers, ", "))
	return approvers, reviewers, nil
}
