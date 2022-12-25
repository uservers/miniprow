package owners

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeOwners(t *testing.T) {
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
  - user3 # user
  - user4 # other user
approvers:
  - approver3 # lead
  - approver4 # lead
`
	// Main test directory
	dir := mkTempRepo(t)
	defer os.RemoveAll(dir)

	// Subdirectory
	require.Nil(t, os.Mkdir(filepath.Join(dir, "sub"), os.FileMode(0o755)))

	// Write owners files
	require.Nil(t, os.WriteFile(filepath.Join(dir, "OWNERS"), []byte(fileData1), os.FileMode(0o644)))
	require.Nil(t, os.WriteFile(filepath.Join(dir, "sub", "OWNERS"), []byte(fileData2), os.FileMode(0o644)))

	impl := &defaultReaderImplementation{}
	owners, err := impl.computeOwners(filepath.Join(dir, "sub"))
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

func mkTempRepo(t *testing.T) string {
	dir, err := os.MkdirTemp("", "owners-test-")
	require.Nil(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), os.FileMode(0o755)))
	require.Nil(t, os.WriteFile(
		filepath.Join(dir, ".git", "config"), []byte("\n"), os.FileMode(0o644),
	))
	return dir
}

func TestReadRespositoryAlias(t *testing.T) {
	testAliases := `aliases:
  release-engineering-approvers:
    - jeefy
    - puerco
    - saschagrunert
  sig-release-leads:
    - cpanato
    - justaugustus
`
	// Create an implementation
	impl := &defaultReaderImplementation{}

	// Create a test directory:
	dir := mkTempRepo(t)
	require.Nil(t, os.Mkdir(filepath.Join(dir, "sub"), os.FileMode(0o755)))
	defer os.RemoveAll(dir)

	// At this point wer have an empty repo. We should get an empty alias list
	list, err := impl.readRespositoryAlias(filepath.Join(dir))
	require.NoError(t, err)
	require.NotNil(t, list)
	require.NotNil(t, list.Aliases)
	require.Len(t, list.Aliases, 0)

	// Write the aliases list
	require.NoError(
		t, os.WriteFile(
			filepath.Join(dir, AliasesFileName), []byte(testAliases), os.FileMode(0o644),
		),
	)

	// Now, lets try several paths
	for _, tc := range []struct {
		path      string
		shouldErr bool
	}{
		{dir, false},                                   // top of repo
		{filepath.Join(dir, "sub"), false},             // subdir
		{filepath.Join(dir, "sub", "hello.txt"), true}, // non existent
		{filepath.Join(dir, AliasesFileName), false},   // file in repo
	} {
		// Read the aliases from the empty dir
		aliases, err := impl.readRespositoryAlias(tc.path)
		if tc.shouldErr {
			require.Nil(t, aliases)
			require.Error(t, err)
			continue
		}

		require.NoError(t, err, "reading empty directory aliases")
		require.Equal(t, 2, len(aliases.Aliases), fmt.Sprintf("%+v", aliases))
	}
}
