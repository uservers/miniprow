// SPDX-FileCopyrightText: 2022 U Servers Comunicaciones, SC
// SPDX-License-Identifier: Apache-2.0

package owners

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

type Options struct{}

func (r *Reader) Options() *Options {
	return r.opts
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
	if extraData == nil {
		return
	}

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

// GetPathOwners analyises a path and returns a list of owners
func (reader *Reader) GetPathOwners(path string) (res *List, err error) {
	// return reader.impl.readPathOwners(path, reader.opts.GetRepoRoot())
	return reader.impl.computeOwners(path)
}

// GetPathOwners analyises a path and returns a list of owners
func (reader *Reader) GetDirectoryOwners(path string) (res *List, err error) {
	return reader.impl.readDirectoryOwners(path)
}

// GetDirectoryAlias returns the aliases in a directoru
func (reader *Reader) GetDirectoryAlias(path string) (res *AliasList, err error) {
	return reader.impl.readRespositoryAlias(path)
}
