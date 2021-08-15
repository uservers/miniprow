package miniprow

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/uServers/miniprow/pkg/github"
)

type SlashCommand struct {
	Command   string   // Command
	Arguments []string // Resto of arguments
	Handler   SlashCommandHandler
}

// Maps comment flags to github labels
var labelMap = map[string]string{
	"lgtm":    "lgtm",
	"approve": "approved",
}

// NewSlashCommandFromLabel builds a slash command from a label and args
func NewSlashCommandFromLabel(label string, args []string) (command SlashCommand, err error) {
	command = SlashCommand{
		Command:   label,
		Arguments: args,
	}
	lmap, err := getLabelMap()
	if err != nil {
		return command, errors.Wrap(err, "reading label map")
	}
	// Check on the label mal if we are dealing with a recognized label
	if _, ok := lmap[label]; ok {
		command.Handler = &labelHandler{
			Label: lmap[label],
			impl:  &defaultHandlerImplementation{},
		}
	}

	// Add other slash command implementations here →

	// /tests-done slash handler. Triggers recheck
	if label == TestsDoneCommand {
		command.Handler = &testsDoneHandler{
			impl: &defaultHandlerImplementation{},
		}
	}

	// Unknown commands use the null handler, only logs the call
	if command.Handler == nil {
		command.Handler = &nullHandler{impl: &defaultHandlerImplementation{}}
	}
	return command, nil
}

// getLabelMap returns the command to label map. Currently hardcoded
func getLabelMap() (map[string]string, error) {
	return labelMap, nil
}

// SlashCommandHandler es la interface de un comando
type SlashCommandHandler interface {
	Run(*Broker, string, []string) error
}

// Execute the slash command
func (slash *SlashCommand) Run(b *Broker) error {
	logrus.Infof("Running /%s slash command handler", slash.Command)
	return slash.Handler.Run(b, slash.Command, slash.Arguments)
}

// labelHandler is a handler that maps adding/removing a label to a PR
type labelHandler struct {
	impl  handlerImplementation
	Label string
}

func (h *labelHandler) Run(b *Broker, commandName string, arguments []string) error {
	if len(arguments) >= 1 && arguments[0] == "cancel" {
		logrus.Infof("Running label handler to remove label %s", h.Label)
		if err := h.impl.removeLabel(b.ctx, h.Label); err != nil {
			return errors.Wrapf(err, "removing label %s from PR", h.Label)
		}
	} else {
		logrus.Infof("Running label handler to add label %s", h.Label)
		if err := h.impl.addLabel(b.ctx, h.Label); err != nil {
			return errors.Wrapf(err, "adding label %s to PR", h.Label)
		}
	}
	return nil
}

type testsDoneHandler struct {
	impl handlerImplementation
}

func (h *testsDoneHandler) Run(b *Broker, commandName string, arguments []string) error {
	return b.CheckMerge()
}

type nullHandler struct {
	impl handlerImplementation
}

func (h *nullHandler) Run(b *Broker, commandName string, arguments []string) error {
	logrus.Warnf("Null slash handler got an unknown command: /%s", commandName)
	return nil
}

// ParseSlashCommands Parse a comment string to look for slash commands
func ParseSlashCommands(commentText string) (commands []SlashCommand, err error) {
	commands = []SlashCommand{}

	lines := strings.Split(commentText, "\n")
	for _, line := range lines {
		tokens := strings.Fields(line)

		// If no words,ignore line
		if len(tokens) == 0 {
			continue
		}

		// If the first word is not a slash command, continue
		if !strings.HasPrefix(tokens[0], "/") || len(tokens[0]) <= 1 {
			continue
		}

		// Build the command array from the tokenized string
		cmd, err := NewSlashCommandFromLabel(strings.TrimPrefix(tokens[0], "/"), tokens[1:])
		if err != nil {
			logrus.Error(errors.Wrapf(err, "while getting command from label %s", tokens[0]))
		}
		commands = append(commands, cmd)

	}
	return commands, err
}

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
//counterfeiter:generate . handlerImplementation
type handlerImplementation interface {
	addLabel(context.Context, string) error
	removeLabel(context.Context, string) error
}

type defaultHandlerImplementation struct{}

func (dhi *defaultHandlerImplementation) addLabel(ctx context.Context, labelName string) error {
	if labelName == "" {
		return errors.New("cannot apply label, got an empty string")
	}
	// chec
	if ctx.Value(ckey).(ContextData).GitHubToken() == "" {
		return errors.New("cannot aplly labels without github token")
	}
	issueID := ctx.Value(ckey).(ContextData).Issue()
	if issueID == 0 {
		issueID = ctx.Value(ckey).(ContextData).PullRequest()
	}
	if issueID == 0 {
		return errors.New("unable to add label, could not get issue ID")
	}

	// Create a github object
	gh, err := github.NewWithToken(ctx.Value(ckey).(ContextData).GitHubToken())
	if err != nil {
		return errors.Wrap(err, "creating github object")
	}

	owner, repo := github.ParseSlug(ctx.Value(ckey).(ContextData).Repository())
	// Get all labels to ensure it exists
	labels, err := gh.ListLabels(owner, repo)
	if err != nil {
		return errors.Wrap(err, "checking if label exists")
	}
	exists := false
	for _, l := range labels {
		if l.GetName() == labelName {
			exists = true
		}
	}

	if !exists {
		// TODO: We should create a comment here notifying the missing label
		return errors.New("cannot apply label " + labelName + " the repository does not have it")
	}

	logrus.Infof("Adding label %s to issue #%d", labelName, issueID)
	if err := gh.AddLabel(owner, repo, issueID, labelName); err != nil {
		return errors.Wrapf(err, "adding label to #%d", issueID)
	}
	logrus.Infof(
		"Added label %s to %s/%s:%d", labelName, owner, repo,
		ctx.Value(ckey).(ContextData).Issue(),
	)
	return nil
}

func (dhi *defaultHandlerImplementation) removeLabel(ctx context.Context, labelName string) error {
	if labelName == "" {
		return errors.New("cannot apply label, got an empty string")
	}
	// chec
	if ctx.Value(ckey).(ContextData).GitHubToken() == "" {
		return errors.New("cannot aplly labels without github token")
	}
	issueID := ctx.Value(ckey).(ContextData).Issue()
	if issueID == 0 {
		issueID = ctx.Value(ckey).(ContextData).PullRequest()
	}
	if issueID == 0 {
		return errors.New("unable to add label, could not get issue ID")
	}

	// Create a github object
	gh, err := github.NewWithToken(ctx.Value(ckey).(ContextData).GitHubToken())
	if err != nil {
		return errors.Wrap(err, "creating github object")
	}

	owner, repo := github.ParseSlug(ctx.Value(ckey).(ContextData).Repository())

	if err := gh.RemoveLabel(owner, repo, issueID, labelName); err != nil {
		return errors.Wrap(err, "removing label")
	}
	return nil
}
