// SPDX-FileCopyrightText: 2022 U Servers Comunicaciones, SC
// SPDX-License-Identifier: Apache-2.0

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
