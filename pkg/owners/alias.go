package owners

type Alias []User

type AliasList struct {
	Aliases map[string]Alias `yaml:"aliases"`
}

func NewAliasList() *AliasList {
	return &AliasList{
		Aliases: map[string]Alias{},
	}
}
