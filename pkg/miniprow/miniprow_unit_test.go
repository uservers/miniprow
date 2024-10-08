package miniprow

import (
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestParseSlashCommands(t *testing.T) {
	text := `Hola soy un comando
	/lgtm
	/lgtm with extra features
	/assign @puerco but no one
	/ not a slash command
	this is not /at the beggining
	/but-this-is
	`
	commands, err := ParseSlashCommands(text)
	require.Nil(t, err, errors.Wrap(err, "parsing slash commands"))

	require.Equal(t, 4, len(commands))
}
