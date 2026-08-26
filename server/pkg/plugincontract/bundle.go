package plugincontract

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/js"
)

// A bundle is what an author publishes: the manifest plus every file the
// manifest names. It is the unit the administrator consents to, so it is parsed
// and validated once, at publish time, and never re-derived from anything the
// author still controls.
//
// Publish-time validation is the point. Under URL hosting, a surface whose entry
// did not exist failed silently in the reader's browser months later; here the
// publish is refused with the missing path named.

const (
	// MaxBundleSize bounds the uploaded archive itself.
	MaxBundleSize = 2 << 20

	// MaxBundleFileSize bounds one file inside it. A surface entry is a single
	// script with its assets inlined, which is what this has to fit.
	MaxBundleFileSize = 1 << 20

	// MaxBundleTotalSize bounds the sum of the files a version keeps. Checked
	// against decompressed sizes as they are read, so an archive that expands
	// far beyond its compressed size is refused rather than buffered.
	MaxBundleTotalSize = 4 << 20

	// MaxBundleEntries bounds how many entries the archive may contain at all,
	// including ones the manifest never names.
	MaxBundleEntries = 512

	// MaxSkillBytes bounds one SKILL.md. Generous for prose, small enough that
	// publishing cannot be used to push a large body into the database.
	MaxSkillBytes = 256 * 1024
)

// BundleFile is one stored file of a published version. Paths are the manifest's
// own relative entries, so nothing has to be re-resolved at serve time.
type BundleFile struct {
	Path    string
	Content []byte
}

// Bundle is a parsed, validated artifact package.
type Bundle struct {
	Manifest Manifest
	// Canonical is the re-encoded manifest an installation stores as its
	// consented snapshot, identical to what ParseManifest returns.
	Canonical []byte
	// Files holds only the entries the manifest references, sorted by path.
	// Anything else in the archive is dropped: nothing can ever serve it, so
	// keeping it would be storage with no read path.
	Files []BundleFile
}

// File returns one file of the bundle by its manifest-relative path.
func (b Bundle) File(entry string) ([]byte, bool) {
	for _, file := range b.Files {
		if file.Path == entry {
			return file.Content, true
		}
	}
	return nil, false
}

// TotalSize is the stored footprint of the bundle's files.
func (b Bundle) TotalSize() int {
	total := 0
	for _, file := range b.Files {
		total += len(file.Content)
	}
	return total
}

// ParseBundle reads a zip archive into a validated bundle.
func ParseBundle(archive []byte) (Bundle, error) {
	if len(archive) == 0 {
		return Bundle{}, fmt.Errorf("plugin package is empty")
	}
	if len(archive) > MaxBundleSize {
		return Bundle{}, fmt.Errorf("plugin package exceeds %d bytes", MaxBundleSize)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return Bundle{}, fmt.Errorf("plugin package must be a zip archive: %w", err)
	}
	if len(reader.File) > MaxBundleEntries {
		return Bundle{}, fmt.Errorf("plugin package contains more than %d entries", MaxBundleEntries)
	}

	entries, err := zipEntryPaths(reader)
	if err != nil {
		return Bundle{}, err
	}
	prefix, err := manifestPrefix(entries)
	if err != nil {
		return Bundle{}, err
	}

	// Files are read lazily, so an archive full of large unreferenced entries
	// costs nothing: only what the manifest names is decompressed, and the
	// running total is checked as each one lands.
	total := 0
	open := func(entry string) ([]byte, bool, error) {
		file, ok := findZipFile(reader, prefix+entry)
		if !ok {
			return nil, false, nil
		}
		content, readErr := readZipFile(file)
		if readErr != nil {
			return nil, false, readErr
		}
		total += len(content)
		if total > MaxBundleTotalSize {
			return nil, false, fmt.Errorf("plugin package files exceed %d bytes", MaxBundleTotalSize)
		}
		return content, true, nil
	}
	return buildBundle(open, prefix)
}

// ParseBundleFromDir builds the same validated bundle from a filesystem
// directory, for the local development channel. `read` is the caller's
// containment-checked reader — this package never touches the filesystem.
func ParseBundleFromDir(read func(entry string) ([]byte, bool, error)) (Bundle, error) {
	total := 0
	open := func(entry string) ([]byte, bool, error) {
		content, ok, err := read(entry)
		if err != nil || !ok {
			return nil, ok, err
		}
		if len(content) > MaxBundleFileSize {
			return nil, false, fmt.Errorf("plugin file %q exceeds %d bytes", entry, MaxBundleFileSize)
		}
		total += len(content)
		if total > MaxBundleTotalSize {
			return nil, false, fmt.Errorf("plugin package files exceed %d bytes", MaxBundleTotalSize)
		}
		return content, true, nil
	}
	return buildBundle(open, "")
}

// buildBundle is the shared half: parse the manifest, then collect exactly the
// files it references. Both publish paths run this, so an uploaded package and a
// local directory can never be validated to different standards.
func buildBundle(open func(entry string) ([]byte, bool, error), prefix string) (Bundle, error) {
	rawManifest, ok, err := open(ManifestFilename)
	if err != nil {
		return Bundle{}, err
	}
	if !ok {
		if prefix != "" {
			return Bundle{}, fmt.Errorf("plugin package must contain %s", prefix+ManifestFilename)
		}
		return Bundle{}, fmt.Errorf("plugin package must contain %s", ManifestFilename)
	}
	manifest, canonical, err := ParseManifest(rawManifest)
	if err != nil {
		return Bundle{}, err
	}

	files := make([]BundleFile, 0, len(manifest.Contributes.Surfaces)+len(manifest.Contributes.Resources)+1)
	seen := map[string]bool{}
	collect := func(entry string) ([]byte, error) {
		if seen[entry] {
			// Two contributions may legitimately name the same file; storing it
			// twice would trip the (version_id, path) unique index.
			content, _ := findFile(files, entry)
			return content, nil
		}
		content, ok, err := open(entry)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("plugin package is missing %q, which the manifest declares", entry)
		}
		if len(content) > MaxBundleFileSize {
			return nil, fmt.Errorf("plugin file %q exceeds %d bytes", entry, MaxBundleFileSize)
		}
		seen[entry] = true
		files = append(files, BundleFile{Path: entry, Content: content})
		return content, nil
	}

	for _, surface := range manifest.Contributes.Surfaces {
		content, err := collect(surface.Entry)
		if err != nil {
			return Bundle{}, err
		}
		if err := validateSurfaceScript(surface.Entry, content); err != nil {
			return Bundle{}, err
		}
	}
	for _, resource := range manifest.Contributes.Resources {
		content, err := collect(resource.Entry)
		if err != nil {
			return Bundle{}, err
		}
		if err := validateSkillFile(resource.Entry, content); err != nil {
			return Bundle{}, err
		}
	}
	if manifest.Icon != "" {
		content, err := collect(manifest.Icon)
		if err != nil {
			return Bundle{}, err
		}
		if len(content) == 0 {
			return Bundle{}, fmt.Errorf("plugin icon %q is empty", manifest.Icon)
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return Bundle{Manifest: manifest, Canonical: canonical, Files: files}, nil
}

func findFile(files []BundleFile, entry string) ([]byte, bool) {
	for _, file := range files {
		if file.Path == entry {
			return file.Content, true
		}
	}
	return nil, false
}

// validateSurfaceScript refuses what would only fail later in a reader's
// browser. The host inlines this text into the surface document, so it has to be
// text, and a static `import` cannot resolve there — a surface is one file with
// no module graph, which is the contract the SDK is shipped for.
func validateSurfaceScript(entry string, content []byte) error {
	if len(bytes.TrimSpace(content)) == 0 {
		return fmt.Errorf("surface entry %q is empty", entry)
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("surface entry %q must be UTF-8 text", entry)
	}
	moduleSyntax, err := parseSurfaceScript(content)
	if err != nil {
		return fmt.Errorf("surface entry %q is not valid JavaScript: %w", entry, err)
	}
	if moduleSyntax {
		return fmt.Errorf("surface entry %q has top-level import/export/await or import.meta; a surface is a single classic script with no module graph, so bundle its dependencies in", entry)
	}
	return nil
}

// parseSurfaceScript uses a JavaScript parser rather than recognizing source
// text by hand. Publishing is the boundary: a false positive blocks an author
// with no workaround, while missed module-only syntax becomes a broken
// classic-script frame. Lexical details such as regex literals, template
// expressions, comments between `import` and `(`, and two statements on one
// line therefore have to follow JavaScript grammar, not a local approximation
// of it.
func parseSurfaceScript(source []byte) (bool, error) {
	ast, err := js.Parse(parse.NewInputBytes(source), js.Options{})
	if err != nil {
		return false, err
	}
	visitor := &surfaceModuleSyntaxVisitor{}
	js.Walk(visitor, ast)
	return visitor.moduleOnly, nil
}

// tdewolff parses a module grammar at the root, where top-level await and
// import.meta are valid. Surface entries execute as classic scripts, so walk
// the parsed tree and reject those constructs explicitly. Await remains valid
// inside a nested async function; import.meta is module-only at every depth.
type surfaceModuleSyntaxVisitor struct {
	functionDepth int
	moduleOnly    bool
}

func (v *surfaceModuleSyntaxVisitor) Enter(node js.INode) js.IVisitor {
	switch node := node.(type) {
	case *js.ImportStmt, *js.ExportStmt, *js.ImportMetaExpr:
		v.moduleOnly = true
	case *js.UnaryExpr:
		if node.Op == js.AwaitToken && v.functionDepth == 0 {
			v.moduleOnly = true
		}
	case *js.FuncDecl, *js.ArrowFunc:
		v.functionDepth++
	}
	return v
}

func (v *surfaceModuleSyntaxVisitor) Exit(node js.INode) {
	switch node.(type) {
	case *js.FuncDecl, *js.ArrowFunc:
		v.functionDepth--
	}
}

func validateSkillFile(entry string, content []byte) error {
	if len(bytes.TrimSpace(content)) == 0 {
		return fmt.Errorf("skill resource %q is empty", entry)
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("skill resource %q must be UTF-8 text", entry)
	}
	if len(content) > MaxSkillBytes {
		return fmt.Errorf("skill resource %q exceeds %d bytes", entry, MaxSkillBytes)
	}
	return nil
}

// zipEntryPaths returns the archive's file paths, refusing anything that could
// escape a directory once written or read by path. The bundle is never unpacked
// to disk, but the paths are matched against manifest entries, and a path that
// normalizes differently than it reads is a way to ship one file and have
// another consented to.
func zipEntryPaths(reader *zip.Reader) ([]string, error) {
	paths := make([]string, 0, len(reader.File))
	for _, file := range reader.File {
		name := file.Name
		if strings.HasSuffix(name, "/") {
			continue // directory entry
		}
		if file.Mode()&fs.ModeSymlink != 0 {
			// Symlinks in a zip carry their target as content. Nothing here
			// follows one, but storing it would publish a file whose meaning
			// depends on a filesystem that does not exist on our side.
			return nil, fmt.Errorf("plugin package must not contain symlinks (%q)", name)
		}
		if strings.ContainsRune(name, '\\') {
			return nil, fmt.Errorf("plugin package entry %q must use forward slashes", name)
		}
		if path.IsAbs(name) || name != path.Clean(name) || strings.HasPrefix(name, "../") {
			return nil, fmt.Errorf("plugin package entry %q must be a plain relative path", name)
		}
		paths = append(paths, name)
	}
	return paths, nil
}

// manifestPrefix finds the directory the manifest sits in.
//
// Zipping a folder is what a person does, and it produces
// `my-plugin/multica.plugin.json`. Accepting exactly one leading directory turns
// the most common publish failure into a non-event; two different roots stay an
// error, because then there is no single answer to what the package is.
func manifestPrefix(paths []string) (string, error) {
	candidates := map[string]bool{}
	for _, name := range paths {
		if name == ManifestFilename {
			return "", nil
		}
		if dir, file := path.Split(name); file == ManifestFilename {
			candidates[dir] = true
		}
	}
	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("plugin package must contain %s", ManifestFilename)
	case 1:
		for dir := range candidates {
			if strings.Count(strings.TrimSuffix(dir, "/"), "/") > 0 {
				return "", fmt.Errorf("%s must be at the root of the package, or one directory below it", ManifestFilename)
			}
			return dir, nil
		}
	}
	return "", fmt.Errorf("plugin package contains more than one %s", ManifestFilename)
}

func findZipFile(reader *zip.Reader, name string) (*zip.File, bool) {
	for _, file := range reader.File {
		if file.Name == name {
			return file, true
		}
	}
	return nil, false
}

func readZipFile(file *zip.File) ([]byte, error) {
	// The archive declares an uncompressed size; it is a claim by whoever built
	// it, so it is used only to refuse early. The read below is bounded on its
	// own and does not trust it.
	if file.UncompressedSize64 > MaxBundleFileSize {
		return nil, fmt.Errorf("plugin file %q exceeds %d bytes", file.Name, MaxBundleFileSize)
	}
	handle, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("read plugin file %q: %w", file.Name, err)
	}
	defer handle.Close()
	content, err := io.ReadAll(io.LimitReader(handle, MaxBundleFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("read plugin file %q: %w", file.Name, err)
	}
	if len(content) > MaxBundleFileSize {
		return nil, fmt.Errorf("plugin file %q exceeds %d bytes", file.Name, MaxBundleFileSize)
	}
	return content, nil
}
