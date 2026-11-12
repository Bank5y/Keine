package course

import (
	"archive/zip"
	"fmt"
	"io"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

const maxManifestSize = 4 << 20

// pkg is an opened course package: the zip's files keyed by their path
// relative to the package root (the folder holding course.json).
type pkg struct {
	files map[string]*zip.File
}

// openPackage indexes a zip. course.json may sit at the root or inside a
// single top-level folder, as when a folder is zipped as a whole.
func openPackage(r *zip.Reader) (*pkg, []Problem) {
	all := map[string]*zip.File{}
	tops := map[string]bool{}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		all[name] = f
		if i := strings.IndexByte(name, '/'); i > 0 && name[:i] != "__MACOSX" {
			tops[name[:i]] = true
		} else if i < 0 {
			tops[""] = true
		}
	}
	root := ""
	if _, ok := all[ManifestFile]; !ok {
		if len(tops) == 1 {
			for t := range tops {
				root = t + "/"
			}
		}
		if _, ok := all[root+ManifestFile]; root == "" || !ok {
			return nil, []Problem{{ManifestFile, "课程包根目录下没有 course.json 课程清单"}}
		}
	}
	p := &pkg{files: map[string]*zip.File{}}
	for name, f := range all {
		if rel, ok := strings.CutPrefix(name, root); ok {
			if clean, ok := cleanPath(rel); ok && clean == rel {
				p.files[rel] = f
			}
		}
	}
	return p, nil
}

func (p *pkg) read(name string, limit int64) ([]byte, error) {
	f, ok := p.files[name]
	if !ok {
		return nil, fmt.Errorf("%s: not in package", name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("%s: larger than %d bytes", name, limit)
	}
	return b, nil
}

// resolve checks a package against its manifest and returns every package
// file the course needs: lesson texts, the images they reference, videos
// and the cover.
func (p *pkg) resolve(m Manifest) ([]string, []Problem) {
	var problems []Problem
	need := map[string]bool{}
	want := func(file, at string) bool {
		if file == "" {
			return false
		}
		if _, ok := p.files[file]; !ok {
			problems = append(problems, Problem{at, fmt.Sprintf("课程包里找不到文件 %q", file)})
			return false
		}
		need[file] = true
		return true
	}
	want(m.Cover, "cover")
	for i, ch := range m.Chapters {
		for j, l := range ch.Lessons {
			at := fmt.Sprintf("chapters[%d].lessons[%d]", i, j)
			want(l.VideoFile, at+".video.file")
			if !want(l.File, at+".file") {
				continue
			}
			text, err := p.read(l.File, maxLessonSize)
			if err != nil {
				problems = append(problems, Problem{at + ".file", "无法读取正文: " + err.Error()})
				continue
			}
			for _, img := range localImages(string(text)) {
				target, ok := resolveRef(l.File, img)
				if !ok {
					problems = append(problems, Problem{l.File, fmt.Sprintf("图片路径 %q 超出了课程包", img)})
					continue
				}
				if _, ok := p.files[target]; !ok {
					problems = append(problems, Problem{l.File, fmt.Sprintf("引用的图片不存在: %s", target)})
					continue
				}
				need[target] = true
			}
		}
	}
	files := make([]string, 0, len(need))
	for f := range need {
		files = append(files, f)
	}
	sort.Strings(files)
	return files, problems
}

const maxLessonSize = 8 << 20

var (
	mdImage  = regexp.MustCompile(`!\[[^\]]*\]\(\s*<?([^)\s>]+)>?(?:\s+["'][^"']*["'])?\s*\)`)
	htmlImg  = regexp.MustCompile(`(?i)<img\b[^>]*?\bsrc\s*=\s*["']([^"']+)["']`)
	hasProto = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

// localImages returns the image references of a Markdown text that point
// into the package (not to the web or data URIs).
func localImages(md string) []string {
	var out []string
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{mdImage, htmlImg} {
		for _, m := range re.FindAllStringSubmatch(md, -1) {
			ref := m[1]
			if hasProto.MatchString(ref) || strings.HasPrefix(ref, "//") || strings.HasPrefix(ref, "#") || seen[ref] {
				continue
			}
			seen[ref] = true
			out = append(out, ref)
		}
	}
	return out
}

// resolveRef resolves an image reference relative to the Markdown file that
// contains it, returning a package-relative path.
func resolveRef(mdFile, ref string) (string, bool) {
	if i := strings.IndexAny(ref, "?#"); i >= 0 {
		ref = ref[:i]
	}
	if u, err := url.PathUnescape(ref); err == nil {
		ref = u
	}
	if strings.HasPrefix(ref, "/") {
		return cleanPath(strings.TrimPrefix(ref, "/"))
	}
	return cleanPath(path.Join(path.Dir(mdFile), ref))
}
