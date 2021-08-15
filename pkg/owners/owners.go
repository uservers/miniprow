package owners

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	yaml "gopkg.in/yaml.v3"
	"sigs.k8s.io/release-utils/util"
)

type Reader struct {
	impl readerImplementation
	opts *Options
}

func NewReader() *Reader {
	r := &Reader{
		impl: &defaultReaderImplementation{},
		opts: DefaultOptions,
	}
	return r
}

type Options struct {
	repoRoot *string
}

func (r *Reader) Options() *Options {
	return r.opts
}

func (o *Options) GetRepoRoot() string {
	if o.repoRoot == nil {
		return ""
	}
	return *o.repoRoot
}

func (o *Options) SetRepoRoot(r string) {
	logrus.Infof("Owners reader repository root set to %s", r)
	o.repoRoot = &r
}

var DefaultOptions = &Options{}

const (
	OwnersFileName  = "OWNERS"
	AliasesFileName = "OWNERS_ALIASES"
)

type List struct {
	Files     []File // List of matching files
	Approvers []User `yaml:"approvers"` // List of approvers found
	Reviewers []User `yaml:"reviewers"` // List of reviewers found
}

// Returns a new empty owners list
func NewList() *List {
	return &List{
		Files:     []File{}, // List of owners files
		Approvers: []User{},
		Reviewers: []User{},
	}
}

// Appends
func (l *List) Append(extraData *List) {
	apps := map[User]struct{}{}
	for _, name := range l.Approvers {
		apps[name] = struct{}{}
	}
	for _, name := range extraData.Approvers {
		if _, ok := apps[name]; !ok {
			apps[name] = struct{}{}
		}
	}
	l.Approvers = []User{}
	for user := range apps {
		l.Approvers = append(l.Approvers, user)
	}

	// Append to the reviewers
	apps = map[User]struct{}{}
	for _, name := range l.Reviewers {
		apps[name] = struct{}{}
	}
	for _, name := range extraData.Reviewers {
		if _, ok := apps[name]; !ok {
			apps[name] = struct{}{}
		}
	}
	l.Reviewers = []User{}
	for user := range apps {
		l.Reviewers = append(l.Reviewers, user)
	}

	// Append to the files
	files := map[string]File{}
	for _, file := range l.Files {
		files[file.Path] = file
	}

	// Cycle the values we're merging
	for _, file := range extraData.Files {
		if _, ok := files[file.Path]; !ok {
			files[file.Path] = file
		}
	}
	l.Files = []File{}
	for _, file := range files {
		l.Files = append(l.Files, file)
	}
}

type User string
type File struct {
	Path      string
	Approvers []User
	Reviewers []User
}

func (f *File) String() string {
	return f.Path
}

// WhoCanApprove gets a list of user handles and returns a list of
// those who can approve the file
func (f *File) WhoCanApprove(userList []string) []string {
	canApprove := []string{}
	// Create inverse map
	inverse := map[string]struct{}{}
	for _, user := range f.Approvers {
		inverse[string(user)] = struct{}{}
	}

	// Check the list and see who can approve
	for _, user := range userList {
		if _, ok := inverse[user]; ok {
			canApprove = append(canApprove, user)
		}
	}

	return canApprove
}

func (u *User) Name() string { return string(*u) }

type Alias []User

type AliasList struct {
	Aliases map[string]Alias `yaml:"aliases"`
}

func NewAliasList() *AliasList {
	return &AliasList{
		Aliases: map[string]Alias{},
	}
}

// GetPathOwners analyises a path and returns a list of owners
func (reader *Reader) GetPathOwners(path string) (res *List, err error) {
	return reader.impl.readPathOwners(path, reader.opts.GetRepoRoot())
}

// GetPathOwners analyises a path and returns a list of owners
func (reader *Reader) GetDirectoryOwners(path string) (res *List, err error) {
	return reader.impl.readDirectoryOwners(path)
}

// GetDirectoryAlias returns the aliases in a directoru
func (reader *Reader) GetDirectoryAlias(path string) (res *AliasList, err error) {
	return reader.impl.readDirectoryAlias(path)
}

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
//counterfeiter:generate . readerImplementation
type readerImplementation interface {
	readDirectoryOwners(string) (*List, error)
	readPathOwners(string, string) (*List, error)
	readDirectoryAlias(string) (*AliasList, error)
	parseAliasFile(string) (*AliasList, error)
	readPathAliases(string, string) (*AliasList, error)
}

type defaultReaderImplementation struct{}

// readPathAliases gets a path and looks for an aliases file
// travering to the top of the repo
func (ri *defaultReaderImplementation) readPathAliases(path, root string) (list *AliasList, err error) {
	partes := strings.Split(path, string(filepath.Separator))
	list = NewAliasList()
	root = strings.TrimSuffix(root, string(filepath.Separator))
	for i := len(partes); i >= 0; i-- {
		testPath := string(filepath.Separator) + filepath.Join(partes[0:i]...)

		// If we got a reporoot, use it to check
		if root != "" {
			if testPath == root {
				return ri.readDirectoryAlias(testPath)
			}
			// Otherwise, try to find the git directory
		} else {
			if util.Exists(filepath.Join(testPath, ".git", "index")) {
				return ri.readDirectoryAlias(testPath)
			}
		}
	}
	logrus.Info("No OWNERS_ALIAS found in repo root")
	return list, nil
}

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

// readDirectoryAlias reads an alias from diredto
func (ri *defaultReaderImplementation) readDirectoryAlias(path string) (list *AliasList, err error) {
	finfo, err := os.Stat(path)
	if err != nil {
		return list, errors.Wrap(err, "opening directory to look for alias file")
	}
	if !finfo.IsDir() {
		return list, errors.New("unable to parse owners, path is not a directory")
	}
	list = NewAliasList()
	if !util.Exists(filepath.Join(path, AliasesFileName)) {
		return list, nil
	}
	return ri.parseAliasFile(filepath.Join(path, AliasesFileName))
}

// readDirectoryOwners gets the owners from a directory
func (ri *defaultReaderImplementation) readDirectoryOwners(path string) (list *List, err error) {
	list = NewList()
	finfo, err := os.Stat(path)
	if err != nil {
		// If the directory does not exist, we return a blank list
		if os.IsNotExist(err) {
			logrus.Infof(
				"expected OWNERS file not found (dir not found): %s",
				filepath.Join(path, OwnersFileName),
			)
			return list, nil
		}
		return list, errors.Wrap(err, "opening directory to look for owners file")
	}
	if !finfo.IsDir() {
		return list, errors.New("unable to parse owners, path is not a directory")
	}

	if !util.Exists(filepath.Join(path, OwnersFileName)) {
		logrus.Infof("expected OWNERS file not found: %s", filepath.Join(path, OwnersFileName))
		return list, nil
	}
	logrus.Infof("Parsing owners file: %s", path)

	yamlData, err := ioutil.ReadFile(filepath.Join(path, OwnersFileName))
	if err != nil {
		return list, errors.Wrap(err, "reading OWNERS YAML data")
	}
	if err := yaml.Unmarshal(yamlData, list); err != nil {
		return list, err
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

	yamlData, err := ioutil.ReadFile(path)
	if err != nil {
		return list, errors.Wrap(err, "reading OWNERS_ALIAS YAML data")
	}
	if err := yaml.Unmarshal(yamlData, list); err != nil {
		return list, err
	}

	return list, nil
}
