package owners

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadPathOwners(t *testing.T) {
	fileData1 := `# See the OWNERS docs at https://go.k8s.io/owners
reviewers:
  - reviewer1 # lead
  - reviewer2 # lead
approvers:
  - approver1 # lead
  - approver2 # lead
  - approver3 # lead
`
	fileData2 := `reviewers:
  - user3 # lead
  - user4 # lead
approvers:
  - approver3 # lead
  - approver4 # lead
`
	// Main test directory
	dir, err := os.MkdirTemp(os.TempDir(), "owners-test-")
	require.Nil(t, err)
	defer os.RemoveAll(dir)
	require.Nil(t, os.WriteFile(filepath.Join(dir, "OWNERS"), []byte(fileData1), os.FileMode(0o644)))

	// Sub directory
	require.Nil(t, os.Mkdir(filepath.Join(dir, "sub"), os.FileMode(0o755)))
	require.Nil(t, os.WriteFile(filepath.Join(dir, "sub", "OWNERS"), []byte(fileData2), os.FileMode(0o644)))

	// Write a fake git directory to stop the traversal
	require.Nil(t, os.Mkdir(filepath.Join(dir, ".git"), os.FileMode(0o755)))
	require.Nil(t, os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("\n"), os.FileMode(0o644)))

	impl := &defaultReaderImplementation{}
	owners, err := impl.readPathOwners(filepath.Join(dir, "sub"), "")
	require.Nil(t, err, err)
	require.Equal(t, 4, len(owners.Reviewers))
	require.Equal(t, 4, len(owners.Approvers))
}

func TestReadDirectoryOwners(t *testing.T) {
	fileData := `# See the OWNERS docs at https://go.k8s.io/owners
reviewers:
  - BenTheElder # lead
  - spiffxp # lead
  - stevekuznetsov # lead
  - test-infra-oncall # oncall
approvers:
  - BenTheElder # lead
  - spiffxp # lead
  - stevekuznetsov # lead
  - test-infra-oncall # oncall
`

	dir, err := os.MkdirTemp(os.TempDir(), "owners-test-")
	require.Nil(t, err)
	defer os.RemoveAll(dir)
	require.Nil(t, os.WriteFile(filepath.Join(dir, "OWNERS"), []byte(fileData), os.FileMode(0o644)))

	impl := &defaultReaderImplementation{}
	owners, err := impl.readDirectoryOwners(dir)
	require.Nil(t, err, err)
	require.Equal(t, 4, len(owners.Reviewers))
	require.Equal(t, 4, len(owners.Approvers))

}

func TestReadPathAliases(t *testing.T) {
	testAliases := `aliases:
  release-engineering-approvers:
    - jeefy
    - puerco
    - saschagrunert
`
	// Create an implementation
	impl := &defaultReaderImplementation{}

	// Create a test directory:
	dir, err := os.MkdirTemp(os.TempDir(), "owners-alias-test-")
	require.Nil(t, err)
	require.Nil(t, os.Mkdir(filepath.Join(dir, "sub"), os.FileMode(0o755)))
	//defer os.RemoveAll(dir)

	// Write a fake git directory to stop the traversal
	require.Nil(t, os.Mkdir(filepath.Join(dir, ".git"), os.FileMode(0o755)))
	require.Nil(t, os.WriteFile(filepath.Join(dir, ".git", "index"), []byte("\n"), os.FileMode(0o644)))

	// Read the aliases from the empty dir
	aliases, err := impl.readPathAliases(dir, "")
	require.Nil(t, err, "reading empty directory aliases")
	require.Equal(t, 0, len(aliases.Aliases))

	require.Nil(t, os.WriteFile(filepath.Join(dir, AliasesFileName), []byte(testAliases), os.FileMode(0o644)))

	// Reading the aliases from the directory, returns the list
	aliases, err = impl.readPathAliases(dir, "")
	require.Nil(t, err)
	require.Len(t, aliases.Aliases, 1)
	_, set := aliases.Aliases["release-engineering-approvers"]
	require.True(t, set)
	require.Len(t, aliases.Aliases["release-engineering-approvers"], 3)

	// Reading the aliases from the directory, returns the list
	aliases, err = impl.readPathAliases(filepath.Join(dir, "sub"), "")
	require.Nil(t, err)
	require.Len(t, aliases.Aliases, 1)
	_, set = aliases.Aliases["release-engineering-approvers"]
	require.True(t, set)
	require.Len(t, aliases.Aliases["release-engineering-approvers"], 3)
}
