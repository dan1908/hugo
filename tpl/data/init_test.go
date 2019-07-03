// Copyright 2017 The Hugo Authors. All rights reserved.
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

package data

import (
	"testing"

	"github.com/gohugoio/hugo/langs"
	"github.com/gohugoio/hugo/tpl/internal"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestInit(t *testing.T) {
	var found bool
	var ns *internal.TemplateFuncsNamespace

	v := viper.New()
	v.Set("contentDir", "content")
	langs.LoadLanguageSettings(v, nil)

	for _, nsf := range internal.TemplateFuncsNamespaceRegistry {
		ns = nsf(newDeps(v))
		if ns.Name == name {
			found = true
			break
		}
	}

	require.True(t, found)
	require.IsType(t, &Namespace{}, ns.Context())
}
