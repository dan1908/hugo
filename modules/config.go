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
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/gohugoio/hugo/config"
	"github.com/gohugoio/hugo/hugofs/files"
	"github.com/gohugoio/hugo/langs"
	"github.com/mitchellh/mapstructure"
)

type Config struct {
	Mounts  []Mount
	Imports []Import
}

type Import struct {
	Path   string // Module path
	Mounts []Mount
}

type Mount struct {
	// TODO(bep) mod add IgnoreFiles: slice of GOB pattern matching content/*
	Source string // relative path in source repo, e.g. "scss"
	Target string // relative target path, e.g. "assets/bootstrap/scss"

	// TODO(bep) mod
	Lang string
}

func (m Mount) Component() string {
	return strings.Split(m.Target, fileSeparator)[0]
}

// DecodeConfig creates a modules Config from a given Hugo configuration.
func DecodeConfig(cfg config.Provider, isMainProject bool) (c Config, err error) {
	if cfg == nil {
		return
	}

	themeSet := cfg.IsSet("theme")
	moduleSet := cfg.IsSet("module")

	// To bridge between old and new configuration format we need
	// a way to make sure all of the core components is configured on
	// the basic level.
	componentsConfigured := make(map[string]bool)

	if moduleSet {
		m := cfg.GetStringMap("module")
		err = mapstructure.WeakDecode(m, &c)

		for i, mnt := range c.Mounts {
			mnt.Source = filepath.Clean(mnt.Source)
			mnt.Target = filepath.Clean(mnt.Target)
			c.Mounts[i] = mnt
			componentsConfigured[path.Join(mnt.Component(), mnt.Source)] = true
		}

	}

	if themeSet {
		imports := config.GetStringSlicePreserveString(cfg, "theme")
		for _, imp := range imports {
			c.Imports = append(c.Imports, Import{
				Path: imp,
			})
		}

	}

	if isMainProject {
		// TODO(bep) mod we need a way to check if contentDir etc. is really set.
		// ... so remove the default settings for these.
		// Map legacy directory config into the new module.
		languages := cfg.Get("languagesSortedDefaultFirst").(langs.Languages)

		type dirKeyComponent struct {
			key       string
			component string
		}

		dirKeys := []dirKeyComponent{
			{"contentDir", files.ComponentFolderContent},
			{"dataDir", files.ComponentFolderData},
			{"layoutDir", files.ComponentFolderLayouts},
			{"i18nDir", files.ComponentFolderI18n},
			{"archetypeDir", files.ComponentFolderArchetypes},
			{"assetDir", files.ComponentFolderAssets},
			{"resourceDir", files.ComponentFolderResources},
		}

		// TODO(bep) mod we add mounts for all languages. May need to revisit/collapse.
		for _, language := range languages {
			for _, dirKey := range dirKeys {
				if language.IsSet(dirKey.key) {
					source := language.GetString(dirKey.key)
					key := path.Join(dirKey.component, source)
					if componentsConfigured[key] {
						continue
					}
					componentsConfigured[key] = true
					c.Mounts = append(c.Mounts, Mount{
						Lang:   language.Lang,
						Source: source,
						Target: dirKey.component})
				}
			}

			// The static dir handling was a little ... special.
			staticDirs := getStaticDirs(language)
			if len(staticDirs) != 0 {
				for _, dir := range staticDirs {
					c.Mounts = append(c.Mounts, Mount{Lang: language.Lang, Source: dir, Target: files.ComponentFolderStatic})
				}
			}
		}

		// Add default configuration
		for _, dirKey := range dirKeys {
			if componentsConfigured[path.Join(dirKey.component, dirKey.component)] {
				continue
			}
			c.Mounts = append(c.Mounts, Mount{Source: dirKey.component, Target: dirKey.component})
		}
	}

	return
}

func removeDuplicatesKeepRight(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for i := len(in) - 1; i >= 0; i-- {
		v := in[i]
		if seen[v] {
			continue
		}
		out = append([]string{v}, out...)
		seen[v] = true
	}

	return out
}

func getStaticDirs(cfg config.Provider) []string {
	var staticDirs []string
	for i := -1; i <= 10; i++ {
		staticDirs = append(staticDirs, getStringOrStringSlice(cfg, "staticDir", i)...)
	}
	return staticDirs
}

func getStringOrStringSlice(cfg config.Provider, key string, id int) []string {

	if id >= 0 {
		key = fmt.Sprintf("%s%d", key, id)
	}

	return config.GetStringSlicePreserveString(cfg, key)

}
