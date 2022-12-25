package owners

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
	"sigs.k8s.io/release-utils/util"
)

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
//counterfeiter:generate . readerImplementation
type readerImplementation interface {
	readDirectoryOwners(string) (*List, error)
	computeOwners(path string) (*List, error)
	parseAliasFile(string) (*AliasList, error)
	readRespositoryAlias(string) (*AliasList, error)
}

type defaultReaderImplementation struct{}

/*
// readPathOwners traverses a path getting owners info until it reaches the git root
func (ri *defaultReaderImplementation) readPathOwners(path, root string) (list *List, err error) {
	logrus.Infof("getting owner data for %s", path)

	// If we are dealing with a file, use its dir as path
	finfo, err := os.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return list, errors.Wrap(err, "opening path to check owners")
	}
	if finfo != nil && !finfo.IsDir() {
		path = filepath.Dir(path)
	}

	partes := strings.Split(path, string(filepath.Separator))
	list = NewList()
	foundRoot := false
	root = strings.TrimSuffix(root, string(filepath.Separator))
	for i := len(partes); i >= 0; i-- {
		testPath := string(filepath.Separator) + filepath.Join(partes[0:i]...)
		sublist, err := ri.readDirectoryOwners(testPath)
		if err != nil {
			return list, errors.Wrapf(err, "getting owners from %s", testPath)
		}
		list.Append(sublist)
		// If we got a root defined, use that to check if we're at the repo top
		if root != "" {
			if root == testPath {
				foundRoot = true
				break
			}
		} else {
			// otherwise try to find the .git dir
			if util.Exists(filepath.Join(testPath, ".git", "index")) {
				foundRoot = true
				break
			}
		}
	}
	if !foundRoot {
		return nil, errors.New("Unable to get owners, could not find repo root")
	}
	return list, nil
}

*/

// readDirectoryAlias returns the alias list
func (ri *defaultReaderImplementation) readRespositoryAlias(path string) (*AliasList, error) {
	finfo, err := os.Stat(path)
	if err != nil {
		return nil, errors.Wrap(err, "opening path to look for alias file")
	}

	if !finfo.IsDir() {
		path = filepath.Dir(path)
	}

	path, err = filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("computing absolute path: %w", err)
	}
	subpath := path
	for {
		if isRepoRoot(subpath) {
			if !util.Exists(filepath.Join(subpath, AliasesFileName)) {
				return NewAliasList(), nil
			}
			return ri.parseAliasFile(filepath.Join(subpath, AliasesFileName))
		}
		subpath = filepath.Dir(subpath)
		subpath, err := filepath.Abs(subpath)
		if err != nil {
			return nil, fmt.Errorf("computing absolute path: %w", err)
		}
		if subpath == "/" {
			break
		}
	}

	return nil, errors.New("unable to find repo root while searching for alias file")
}

// computeOwners gets a path and traverses the directory finding
// the required approvers and reviewers
func (ri *defaultReaderImplementation) computeOwners(path string) (*List, error) {
	var list *List
	finfo, err := os.Stat(path)
	if err != nil {
		return list, fmt.Errorf("path not found when computing owners: %w", err)
	}
	if !finfo.IsDir() {
		path = filepath.Dir(path)
	}

	_, err = ri.readRespositoryAlias(path)
	if err != nil {
		return nil, fmt.Errorf("reading repo aliases: %w", err)
	}
	subpath := path
	for {
		// We need to make sure we don't traverse all the way up to root
		subpath, err = filepath.Abs(subpath)
		if err != nil {
			return nil, fmt.Errorf("computing absolute path: %w", err)
		}
		if subpath == "/" {
			return nil, fmt.Errorf("unable to detect repository root")
		}

		if util.Exists(filepath.Join(subpath, OwnersFileName)) {
			localList, err := ri.readDirectoryOwners(subpath)
			if err != nil {
				return nil, fmt.Errorf("parsing owners file in path: %w", err)
			}

			if list == nil {
				list = &List{}
			}

			list.Append(localList)
		}
		if isRepoRoot(subpath) {
			break
		}
		subpath = filepath.Dir(subpath)
	}

	if list == nil {
		return nil, fmt.Errorf("unable to find any approvers for path")
	}
	return list, nil
}

// isRepoRoot is a utility function that returns true if a dir is the root of a repository
func isRepoRoot(path string) bool {
	return util.Exists(filepath.Join(path, ".git/config"))
}

// readDirectoryOwners gets the owners from a directory
func (ri *defaultReaderImplementation) readDirectoryOwners(path string) (list *List, err error) {
	list = NewList()

	finfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("opening directory to look for owners file: %w", err)
	}
	if !finfo.IsDir() {
		return nil, errors.New("unable to parse owners, path is not a directory")
	}

	if !util.Exists(filepath.Join(path, OwnersFileName)) {
		logrus.Infof("OWNERS file not found: %s", filepath.Join(path, OwnersFileName))
		return nil, nil
	}

	logrus.Infof("Parsing owners file: %s", path)

	yamlData, err := os.ReadFile(filepath.Join(path, OwnersFileName))
	if err != nil {
		return list, fmt.Errorf("reading OWNERS YAML data: %w", err)
	}
	if err := yaml.Unmarshal(yamlData, list); err != nil {
		return list, fmt.Errorf("unmarshaling OWNERS data: %w", err)
	}

	// Build the file entry
	f := File{
		Path:      filepath.Join(path, OwnersFileName),
		Approvers: list.Approvers,
		Reviewers: list.Reviewers,
	}
	list.Files = append(list.Files, f)
	return list, nil
}

// parseAliasFile parses an OWNERS_ALIAS file
func (ri *defaultReaderImplementation) parseAliasFile(path string) (list *AliasList, err error) {
	if !util.Exists(path) {
		return nil, errors.Wrap(err, "file not found")
	}
	list = NewAliasList()

	yamlData, err := os.ReadFile(path)
	if err != nil {
		return list, errors.Wrap(err, "reading OWNERS_ALIAS YAML data")
	}
	if err := yaml.Unmarshal(yamlData, list); err != nil {
		return list, err
	}

	return list, nil
}
