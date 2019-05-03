// Copyright 2018 The Hugo Authors. All rights reserved.
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

// Package filesystems provides the fine grained file systems used by Hugo. These
// are typically virtual filesystems that are composites of project and theme content.
package filesystems

import (
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gohugoio/hugo/hugofs/files"

	"github.com/pkg/errors"

	"github.com/gohugoio/hugo/modules"

	"github.com/gohugoio/hugo/config"

	"github.com/gohugoio/hugo/hugofs"

	"fmt"

	"github.com/gohugoio/hugo/hugolib/paths"
	"github.com/gohugoio/hugo/langs"
	"github.com/spf13/afero"
)

var filePathSeparator = string(filepath.Separator)

// BaseFs contains the core base filesystems used by Hugo. The name "base" is used
// to underline that even if they can be composites, they all have a base path set to a specific
// resource folder, e.g "/my-project/content". So, no absolute filenames needed.
type BaseFs struct {

	// SourceFilesystems contains the different source file systems.
	*SourceFilesystems

	// The filesystem used to publish the rendered site.
	// This usually maps to /my-project/public.
	PublishFs afero.Fs

	theBigFs *filesystemsCollector

	// TODO(bep) improve the "theme interaction"
	AbsThemeDirs []string
}

func (fs *BaseFs) WatchDirs() []hugofs.FileMetaInfo {
	var dirs []hugofs.FileMetaInfo
	for _, dir := range fs.AllDirs() {
		if dir.Meta().Watch() {
			dirs = append(dirs, dir)
		}
	}

	return dirs
}

func (fs *BaseFs) AllDirs() []hugofs.FileMetaInfo {
	var dirs []hugofs.FileMetaInfo
	for _, dirSet := range [][]hugofs.FileMetaInfo{
		fs.Archetypes.Dirs,
		fs.I18n.Dirs,
		fs.Data.Dirs,
		fs.Content.Dirs,
		fs.Assets.Dirs,
		fs.Resources.Dirs,
	} {
		dirs = append(dirs, dirSet...)
	}

	return dirs
}

// RelContentDir tries to create a path relative to the content root from
// the given filename. The return value is the path and language code.
func (b *BaseFs) RelContentDir(filename string) string {
	for _, dir := range b.SourceFilesystems.Content.Dirs {
		dirname := dir.Meta().Filename()
		if strings.HasPrefix(filename, dirname) {
			rel := strings.TrimPrefix(filename, dirname)
			return strings.TrimPrefix(rel, filePathSeparator)
		}
	}
	// Either not a content dir or already relative.
	return filename
}

// SourceFilesystems contains the different source file systems. These can be
// composite file systems (theme and project etc.), and they have all root
// set to the source type the provides: data, i18n, static, layouts.
type SourceFilesystems struct {
	Content    *SourceFilesystem
	Data       *SourceFilesystem
	I18n       *SourceFilesystem
	Layouts    *SourceFilesystem
	Archetypes *SourceFilesystem
	Assets     *SourceFilesystem
	Resources  *SourceFilesystem

	// Writable filesystem to the project's resources directory.
	ResourcesCache afero.Fs

	// The project folder.
	Work afero.Fs

	// When in multihost we have one static filesystem per language. The sync
	// static files is currently done outside of the Hugo build (where there is
	// a concept of a site per language).
	// When in non-multihost mode there will be one entry in this map with a blank key.
	Static map[string]*SourceFilesystem
}

// A SourceFilesystem holds the filesystem for a given source type in Hugo (data,
// i18n, layouts, static) and additional metadata to be able to use that filesystem
// in server mode.
// TODO(bep) mod clean up
type SourceFilesystem struct {
	// This is a virtual composite filesystem. It expects path relative to a context.
	Fs afero.Fs

	// This filesystem as separate root directories, starting from project and down
	// to the themes/modules.
	Dirs []hugofs.FileMetaInfo

	// This is the base source filesystem. In real Hugo, this will be the OS filesystem.
	// Use this if you need to resolve items in Dirnames below.
	// TODO(bep) mod rethink this, also vs themes and symlinks.
	SourceFs afero.Fs

	// Dirnames is absolute filenames to the directories in this filesystem.
	Dirnames []string

	// When syncing a source folder to the target (e.g. /public), this may
	// be set to publish into a subfolder. This is used for static syncing
	// in multihost mode.
	PublishFolder string
}

// ContentStaticAssetFs will create a new composite filesystem from the content,
// static, and asset filesystems. The site language is needed to pick the correct static filesystem.
// The order is content, static and then assets.
// TODO(bep) check usage
func (s SourceFilesystems) ContentStaticAssetFs(lang string) afero.Fs {
	staticFs := s.StaticFs(lang)

	base := afero.NewCopyOnWriteFs(s.Assets.Fs, staticFs)
	return afero.NewCopyOnWriteFs(base, s.Content.Fs)

}

// StaticFs returns the static filesystem for the given language.
// This can be a composite filesystem.
func (s SourceFilesystems) StaticFs(lang string) afero.Fs {
	var staticFs afero.Fs = hugofs.NoOpFs

	if fs, ok := s.Static[lang]; ok {
		staticFs = fs.Fs
	} else if fs, ok := s.Static[""]; ok {
		staticFs = fs.Fs
	}

	return staticFs
}

// StatResource looks for a resource in these filesystems in order: static, assets and finally content.
// If found in any of them, it returns FileInfo and the relevant filesystem.
// Any non os.IsNotExist error will be returned.
// An os.IsNotExist error wil be returned only if all filesystems return such an error.
// Note that if we only wanted to find the file, we could create a composite Afero fs,
// but we also need to know which filesystem root it lives in.
func (s SourceFilesystems) StatResource(lang, filename string) (fi os.FileInfo, fs afero.Fs, err error) {
	for _, fsToCheck := range []afero.Fs{s.StaticFs(lang), s.Assets.Fs, s.Content.Fs} {
		fs = fsToCheck
		fi, err = fs.Stat(filename)
		if err == nil || !os.IsNotExist(err) {
			return
		}
	}
	// Not found.
	return
}

// IsStatic returns true if the given filename is a member of one of the static
// filesystems.
func (s SourceFilesystems) IsStatic(filename string) bool {
	for _, staticFs := range s.Static {
		if staticFs.Contains(filename) {
			return true
		}
	}
	return false
}

// IsContent returns true if the given filename is a member of the content filesystem.
func (s SourceFilesystems) IsContent(filename string) bool {
	return s.Content.Contains(filename)
}

// IsLayout returns true if the given filename is a member of the layouts filesystem.
func (s SourceFilesystems) IsLayout(filename string) bool {
	return s.Layouts.Contains(filename)
}

// IsData returns true if the given filename is a member of the data filesystem.
func (s SourceFilesystems) IsData(filename string) bool {
	return s.Data.Contains(filename)
}

// IsAsset returns true if the given filename is a member of the asset filesystem.
func (s SourceFilesystems) IsAsset(filename string) bool {
	return s.Assets.Contains(filename)
}

// IsI18n returns true if the given filename is a member of the i18n filesystem.
func (s SourceFilesystems) IsI18n(filename string) bool {
	return s.I18n.Contains(filename)
}

// MakeStaticPathRelative makes an absolute static filename into a relative one.
// It will return an empty string if the filename is not a member of a static filesystem.
func (s SourceFilesystems) MakeStaticPathRelative(filename string) string {
	for _, staticFs := range s.Static {
		rel := staticFs.MakePathRelative(filename)
		if rel != "" {
			return rel
		}
	}
	return ""
}

// MakePathRelative creates a relative path from the given filename.
// It will return an empty string if the filename is not a member of this filesystem.
func (d *SourceFilesystem) MakePathRelative(filename string) string {
	for _, dir := range d.Dirs {
		currentPath := dir.(hugofs.FileMetaInfo).Meta().Filename()
		if strings.HasPrefix(filename, currentPath) {
			return strings.TrimPrefix(filename, currentPath)
		}
	}
	return ""
}

func (d *SourceFilesystem) RealFilename(rel string) string {
	fi, err := d.Fs.Stat(rel)
	if err != nil {
		return rel
	}
	if realfi, ok := fi.(hugofs.FileMetaInfo); ok {
		return realfi.Meta().Filename()
	}

	return rel
}

// Contains returns whether the given filename is a member of the current filesystem.
func (d *SourceFilesystem) Contains(filename string) bool {
	for _, dir := range d.Dirs {
		if strings.HasPrefix(filename, dir.Meta().Filename()) {
			return true
		}
	}
	return false
}

// RealDirs gets a list of absolute paths to directories starting from the given
// path.
func (d *SourceFilesystem) RealDirs(from string) []string {
	var dirnames []string
	for _, dir := range d.Dirs {
		meta := dir.Meta()
		dirname := filepath.Join(meta.Filename(), from)
		_, err := meta.Fs().Stat(from)

		if err == nil {
			dirnames = append(dirnames, dirname)
		}
	}
	return dirnames
}

// WithBaseFs allows reuse of some potentially expensive to create parts that remain
// the same across sites/languages.
func WithBaseFs(b *BaseFs) func(*BaseFs) error {
	return func(bb *BaseFs) error {
		bb.theBigFs = b.theBigFs
		bb.AbsThemeDirs = b.AbsThemeDirs
		return nil
	}
}

// NewBase builds the filesystems used by Hugo given the paths and options provided.NewBase
func NewBase(p *paths.Paths, options ...func(*BaseFs) error) (*BaseFs, error) {
	fs := p.Fs

	publishFs := afero.NewBasePathFs(fs.Destination, p.AbsPublishDir)

	b := &BaseFs{
		PublishFs: publishFs,
	}

	for _, opt := range options {
		if err := opt(b); err != nil {
			return nil, err
		}
	}

	builder := newSourceFilesystemsBuilder(p, b)
	sourceFilesystems, err := builder.Build()
	if err != nil {
		return nil, errors.Wrap(err, "build filesystems")
	}

	b.SourceFilesystems = sourceFilesystems
	b.theBigFs = builder.theBigFs
	// TODO(bep) mod	b.AbsThemeDirs = builder.absThemeDirs

	return b, nil
}

type sourceFilesystemsBuilder struct {
	p        *paths.Paths
	sourceFs afero.Fs
	result   *SourceFilesystems
	theBigFs *filesystemsCollector
}

func newSourceFilesystemsBuilder(p *paths.Paths, b *BaseFs) *sourceFilesystemsBuilder {
	sourceFs := hugofs.NewBaseFileDecorator(p.Fs.Source)
	return &sourceFilesystemsBuilder{p: p, sourceFs: sourceFs, theBigFs: b.theBigFs, result: &SourceFilesystems{}}
}

func (b *sourceFilesystemsBuilder) newSourceFilesystem(fs afero.Fs, dirs []hugofs.FileMetaInfo) *SourceFilesystem {
	return &SourceFilesystem{
		Fs:       fs,
		Dirs:     dirs,
		SourceFs: b.sourceFs,
	}
}
func (b *sourceFilesystemsBuilder) Build() (*SourceFilesystems, error) {

	if b.theBigFs == nil {

		theBigFs, err := b.createMainOverlayFs(b.p)
		if err != nil {
			return nil, errors.Wrap(err, "create main fs")
		}
		if theBigFs.overlayMounts == nil {
			panic("overlayMounts was nil")
		}
		b.theBigFs = theBigFs
	}

	createView := func(componentID string) *SourceFilesystem {
		if b.theBigFs == nil {
			panic("no fs")
		}
		if b.theBigFs.overlayMounts == nil {
			panic("no overlay")
		}

		dirs := b.theBigFs.overlayDirs[componentID]

		return b.newSourceFilesystem(afero.NewBasePathFs(b.theBigFs.overlayMounts, componentID), dirs)

	}

	b.theBigFs.finalizeDirs()

	b.result.Archetypes = createView(files.ComponentFolderArchetypes)
	b.result.Layouts = createView(files.ComponentFolderLayouts)
	b.result.Assets = createView(files.ComponentFolderAssets)
	b.result.Resources = createView(files.ComponentFolderResources)
	b.result.ResourcesCache = afero.NewBasePathFs(b.sourceFs, b.p.AbsResourcesDir)

	// Data, i18n and content cannot use the overlay fs
	dataDirs := b.theBigFs.overlayDirs[files.ComponentFolderData]
	dataFs, err := hugofs.NewSliceFs(dataDirs...)
	if err != nil {
		return nil, err
	}

	b.result.Data = b.newSourceFilesystem(dataFs, dataDirs)

	i18nDirs := b.theBigFs.overlayDirs[files.ComponentFolderI18n]
	i18nFs, err := hugofs.NewSliceFs(i18nDirs...)
	if err != nil {
		return nil, err
	}
	b.result.I18n = b.newSourceFilesystem(i18nFs, i18nDirs)

	contentDirs := b.theBigFs.overlayDirs[files.ComponentFolderContent]
	contentBfs := afero.NewBasePathFs(b.theBigFs.overlayMounts, files.ComponentFolderContent)

	contentFs, err := hugofs.NewLanguageFs(b.p.Languages.AsSet(), contentBfs)
	if err != nil {
		return nil, errors.Wrap(err, "create content filesystem")
	}

	b.result.Content = b.newSourceFilesystem(contentFs, contentDirs)

	b.result.Work = afero.NewReadOnlyFs(b.theBigFs.overlayFull)

	// Create static filesystem(s)
	ms := make(map[string]*SourceFilesystem)
	b.result.Static = ms

	staticDirs := b.theBigFs.overlayDirs[files.ComponentFolderStatic]

	if b.theBigFs.staticPerLanguage != nil {
		// Multihost mode
		for k, v := range b.theBigFs.staticPerLanguage {
			// TODO(bep) mod check the usage of staticDirs, this is all.
			ms[k] = b.newSourceFilesystem(v, staticDirs)
		}
	} else {
		bfs := afero.NewBasePathFs(b.theBigFs.overlayMounts, files.ComponentFolderStatic)
		ms[""] = b.newSourceFilesystem(bfs, staticDirs)
	}

	return b.result, nil

}

func (b *sourceFilesystemsBuilder) existsInSource(abspath string) bool {
	exists, _ := afero.Exists(b.sourceFs, abspath)
	return exists
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

// TODO(bep) mod remove
func (b *sourceFilesystemsBuilder) createContentMounts() ([]modules.Mount, error) {

	defaultContentLanguage := b.p.DefaultContentLanguage
	languages := b.p.Languages

	var contentLanguages langs.Languages
	var contentDirSeen = make(map[string]bool)
	languageSet := make(map[string]bool)

	// The default content language needs to be first.
	for _, language := range languages {
		if language.Lang == defaultContentLanguage {
			contentLanguages = append(contentLanguages, language)
			contentDirSeen[language.ContentDir] = true
		}
		languageSet[language.Lang] = true
	}

	for _, language := range languages {
		if contentDirSeen[language.ContentDir] {
			continue
		}
		if language.ContentDir == "" {
			language.ContentDir = defaultContentLanguage
		}
		contentDirSeen[language.ContentDir] = true
		contentLanguages = append(contentLanguages, language)

	}

	mounts := make([]modules.Mount, len(contentLanguages))

	for i, language := range contentLanguages {
		mounts[i] = modules.Mount{
			Source: language.ContentDir,
			Target: files.ComponentFolderContent,
			Lang:   language.Lang,
		}
	}

	return mounts, nil

}

func (b *sourceFilesystemsBuilder) createMainOverlayFs(p *paths.Paths) (*filesystemsCollector, error) {

	mods := p.AllModules

	if len(mods) == 0 {
		panic("no mods set")
	}

	modsReversed := make([]mountsDescriptor, len(mods))

	// The theme components are ordered from left to right.
	// We need to revert it to get the
	// overlay logic below working as expected, with the project on top (last).

	for i, mod := range mods {
		dir := mod.Dir()

		if i < len(mods)-1 {
			i = len(mods) - 2 - i
		}

		modsReversed[i] = mountsDescriptor{
			mounts:        mod.Mounts(),
			dir:           dir,
			watch:         !mod.IsGoMod(), // TODO(bep) mod consider Replace
			isMainProject: mod.Owner() == nil,
		}
	}

	var staticFsMap map[string]afero.Fs
	if b.p.Cfg.GetBool("multihost") {
		staticFsMap = make(map[string]afero.Fs)
	}

	collector := &filesystemsCollector{
		sourceProject:     b.sourceFs,
		sourceModules:     b.sourceFs, // TODO(bep) mod: add a no symlinc fs
		overlayDirs:       make(map[string][]hugofs.FileMetaInfo),
		staticPerLanguage: staticFsMap,
	}

	err := b.createOverlayFs(collector, modsReversed)

	return collector, err

}

func (b *sourceFilesystemsBuilder) isContentMount(mnt modules.Mount) bool {
	return strings.HasPrefix(mnt.Target, "content")
}

func (b *sourceFilesystemsBuilder) createModFs(
	collector *filesystemsCollector,
	md mountsDescriptor) error {

	var fromTo []hugofs.RootMapping

	absPathify := func(path string) string {
		return paths.AbsPathify(md.dir, path)
	}

	seen := make(map[string]bool)

	var mounts []modules.Mount

OUTER:
	for i, mount := range md.mounts {
		key := path.Join(mount.Source, mount.Target)
		if seen[key] {
			continue
		}
		seen[key] = true

		// Prevent overlapping mounts
		for j, mount2 := range md.mounts {
			if j == i || mount2.Target != mount.Target {
				continue
			}
			if strings.HasPrefix(mount2.Source, mount.Source) {
				continue OUTER
			}
		}

		mounts = append(mounts, mount)
	}

	for _, mount := range mounts {

		mountWeight := 1
		if md.isMainProject {
			mountWeight++
		}

		rm := hugofs.RootMapping{
			From: mount.Target,
			To:   absPathify(mount.Source),
			Meta: hugofs.FileMeta{
				"watch":       md.watch,
				"mountWeight": mountWeight,
			},
		}

		lang := mount.Lang
		if lang == "" && b.isContentMount(mount) {
			lang = b.p.DefaultContentLanguage
		}

		rm.Meta["lang"] = lang

		fromTo = append(fromTo, rm)
	}

	modBase := collector.sourceProject
	if !md.isMainProject {
		modBase = collector.sourceModules
	}

	rmfs, err := hugofs.NewRootMappingFs(modBase, fromTo...)
	if err != nil {
		return err
	}

	// We need to keep the ordered list of directories for watching and
	// some special merge operations (data, i18n).
	collector.addDirs(rmfs)

	// TODO(bep) mod
	if collector.staticPerLanguage != nil {
		for _, l := range b.p.Languages {
			lang := l.Lang

			lfs := rmfs.Filter(func(rm hugofs.RootMapping) bool {
				rlang := rm.Meta.Lang()
				return rlang == "" || rlang == lang
			})

			bfs := afero.NewBasePathFs(lfs, files.ComponentFolderStatic)

			sfs, found := collector.staticPerLanguage[lang]
			if found {
				collector.staticPerLanguage[lang] = afero.NewCopyOnWriteFs(sfs, bfs)

			} else {
				collector.staticPerLanguage[lang] = bfs
			}
		}
	}

	if collector.overlayMounts == nil {
		collector.overlayMounts = rmfs
		collector.overlayFull = afero.NewBasePathFs(modBase, md.dir)
	} else {
		//collector.overlayMounts = afero.NewCopyOnWriteFs(collector.overlayMounts, rmfs)
		collector.overlayMounts = hugofs.NewLanguageCompositeFs(collector.overlayMounts, rmfs)
		collector.overlayFull = afero.NewCopyOnWriteFs(collector.overlayFull, afero.NewBasePathFs(modBase, md.dir))
	}

	return nil

}

func printFs(fs afero.Fs, path string, w io.Writer) {
	if fs == nil {
		return
	}
	afero.Walk(fs, path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		var filename string
		if fim, ok := info.(hugofs.FileMetaInfo); ok {
			filename = fim.Meta().Filename()
		}
		fmt.Fprintf(w, "    %q %q\n", path, filename)
		return nil
	})
}

type filesystemsCollector struct {
	sourceProject afero.Fs // Source for project folders
	sourceModules afero.Fs // Source for modules/themes

	overlayMounts afero.Fs
	overlayFull   afero.Fs

	// Maps component type (layouts, static, content etc.) an ordered list of
	// directories representing the overlay filesystems above.
	overlayDirs map[string][]hugofs.FileMetaInfo

	// Set if in multihost mode
	staticPerLanguage map[string]afero.Fs

	finalizerInit sync.Once
}

func (c *filesystemsCollector) addDirs(rfs *hugofs.RootMappingFs) {
	for _, componentFolder := range files.ComponentFolders {
		dirs, err := rfs.Dirs(componentFolder)
		if err == nil {
			c.overlayDirs[componentFolder] = append(c.overlayDirs[componentFolder], dirs...)
		}
	}
}

func (c *filesystemsCollector) finalizeDirs() {
	// TODO(bep) mod really need to understand why this is called > 1 time.
	c.finalizerInit.Do(func() {
		// Order the directories from top to bottom (project, theme a, theme ...).
		for _, dirs := range c.overlayDirs {
			c.reverseFis(dirs)
		}
	})

}

func (c *filesystemsCollector) reverseFis(fis []hugofs.FileMetaInfo) {
	for i := len(fis)/2 - 1; i >= 0; i-- {
		opp := len(fis) - 1 - i
		fis[i], fis[opp] = fis[opp], fis[i]
	}
}

type mountsDescriptor struct {
	mounts        []modules.Mount
	dir           string
	watch         bool // whether this is a candidate for watching in server mode.
	isMainProject bool
}

func (b *sourceFilesystemsBuilder) createOverlayFs(collector *filesystemsCollector, mounts []mountsDescriptor) error {
	if len(mounts) == 0 {
		return nil
	}

	err := b.createModFs(collector, mounts[0])
	if err != nil {
		return err
	}

	if len(mounts) == 1 {
		return nil
	}

	return b.createOverlayFs(collector, mounts[1:])
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
