// Copyright 2019 The Hugo Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package modules

import (
	"testing"

	"github.com/gohugoio/hugo/langs"

	"github.com/gohugoio/hugo/config"

	"github.com/stretchr/testify/require"
)

func TestDecodeConfig(t *testing.T) {
	assert := require.New(t)
	tomlConfig := `
[module]
[[module.mounts]]
source="src/project/blog"
target="content/blog"
lang="en"
[[module.imports]]
path="github.com/bep/mycomponent"
[[module.imports.mounts]]
source="scss"
target="assets/bootstrap/scss"
[[module.imports.mounts]]
source="src/markdown/blog"
target="content/blog"
lang="en"
`
	cfg, err := config.FromConfigString(tomlConfig, "toml")
	assert.NoError(err)

	mcfg, err := DecodeConfig(cfg, false)
	assert.NoError(err)

	assert.Len(mcfg.Mounts, 1)
	assert.Len(mcfg.Imports, 1)
	imp := mcfg.Imports[0]
	imp.Path = "github.com/bep/mycomponent"
	assert.Equal("src/markdown/blog", imp.Mounts[1].Source)
	assert.Equal("content/blog", imp.Mounts[1].Target)
	assert.Equal("en", imp.Mounts[1].Lang)

}

// Test old style theme import.
func TestDecodeConfigTheme(t *testing.T) {
	assert := require.New(t)
	tomlConfig := `

theme = ["a", "b"]
`
	cfg, err := config.FromConfigString(tomlConfig, "toml")
	assert.NoError(err)

	mcfg, err := DecodeConfig(cfg, false)
	assert.NoError(err)

	assert.Len(mcfg.Imports, 2)
	assert.Equal("a", mcfg.Imports[0].Path)
	assert.Equal("b", mcfg.Imports[1].Path)
}

func TestDecodeConfigBothOldAndNewProvided(t *testing.T) {
	assert := require.New(t)
	tomlConfig := `

theme = ["b", "c"]

[module]
[[module.imports]]
path="a"

`
	cfg, err := config.FromConfigString(tomlConfig, "toml")
	assert.NoError(err)

	modCfg, err := DecodeConfig(cfg, false)
	assert.NoError(err)
	assert.Len(modCfg.Imports, 3)
	assert.Equal("a", modCfg.Imports[0].Path)

}

func TestDecodeConfigMainProjectWithLegacySettings(t *testing.T) {
	assert := require.New(t)

	tomlConfig := `
defaultContentLanguage="en"
contentDir="mycontent"
dataDir="mydata"
staticDir=["static1", "static2"]
layoutDir="mylayouts"
i18nDir="myi18n"

[module]
[[module.mounts]]
source="src/project/blog"
target="content/blog"
lang="en"

[languages]
[languages.en]
baseURL = "https://example.com"
languageName = "English"
staticDir2 = "static_en"
title = "In English"
weight = 2
[languages.no]
baseURL = "https://example.no"
languageName = "Norsk"
staticDir = ["staticDir_override", "static_no"]
title = "På norsk"
weight = 1
`

	cfg, err := config.FromConfigString(tomlConfig, "toml")
	assert.NoError(err)
	_, err = langs.LoadLanguageSettings(cfg, nil)
	assert.NoError(err)

	modCfg, err := DecodeConfig(cfg, true)
	assert.NoError(err)

	//fmt.Println(litter.Sdump(modCfg))
	assert.Len(modCfg.Mounts, 9)

}
