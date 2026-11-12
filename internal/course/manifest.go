// Package course owns courses, their chapters and lessons, and the import of
// course packages described by a course manifest (ADR-0002).
package course

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/Bank5y/keine/internal/storage"
)

const (
	ManifestFile    = "course.json"
	ManifestVersion = 1

	TypeColumn = "column"
	TypeVideo  = "video"
	TypePaper  = "paper"

	Finished   = "finished"
	Ongoing    = "ongoing"
	Incomplete = "incomplete"

	maxKeyLength   = 64
	maxTitleLength = 200
)

// Manifest is a validated course manifest.
type Manifest struct {
	Title       string
	Type        string
	Completion  string
	Intro       string
	AuthorName  string
	AuthorIntro string
	Cover       string // package-relative path, or ""
	Chapters    []Chapter
}

type Chapter struct {
	Key     string
	Title   string
	Lessons []Lesson
}

type Lesson struct {
	Key       string
	Title     string
	File      string // package-relative Markdown text, or ""
	VideoFile string // package-relative video, or ""
	VideoURL  string // external video link, or ""
	Duration  string
}

// Problem is one thing wrong with a course package. Path points into the
// manifest ("chapters[1].lessons[0].file") or names a package file.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// InvalidPackage is returned when a package cannot be imported; it lists
// every problem found, not just the first.
type InvalidPackage struct{ Problems []Problem }

func (e *InvalidPackage) Error() string {
	return fmt.Sprintf("invalid course package: %d problem(s)", len(e.Problems))
}

// ParseManifest decodes and validates a manifest. It checks the manifest on
// its own; whether referenced files exist is checked against the package.
func ParseManifest(data []byte) (Manifest, []Problem) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return Manifest{}, []Problem{{ManifestFile, "不是合法的 JSON: " + err.Error()}}
	}
	v := &validator{}
	m := v.manifest(root)
	return m, v.problems
}

type validator struct{ problems []Problem }

func (v *validator) add(path, format string, args ...any) {
	v.problems = append(v.problems, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func join(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// object checks that x is a JSON object with only known keys.
func (v *validator) object(x any, at string, known ...string) (map[string]any, bool) {
	m, ok := x.(map[string]any)
	if !ok {
		v.add(at, "必须是 JSON 对象")
		return nil, false
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !contains(known, k) {
			v.add(join(at, k), "未知字段 %q (可用字段: %s)", k, strings.Join(known, ", "))
		}
	}
	return m, true
}

// str reads an optional string field; required fields must be non-blank.
func (v *validator) str(m map[string]any, key, at string, required bool) string {
	x, present := m[key]
	if !present || x == nil {
		if required {
			v.add(join(at, key), "缺少必填字段")
		}
		return ""
	}
	s, ok := x.(string)
	if !ok {
		v.add(join(at, key), "必须是字符串")
		return ""
	}
	if required && strings.TrimSpace(s) == "" {
		v.add(join(at, key), "不能为空")
	}
	return strings.TrimSpace(s)
}

func (v *validator) manifest(root any) Manifest {
	var m Manifest
	obj, ok := v.object(root, "", "version", "title", "type", "completion", "intro", "author", "cover", "chapters")
	if !ok {
		v.problems[len(v.problems)-1].Path = ManifestFile
		return m
	}
	if n, ok := obj["version"].(json.Number); !ok || n.String() != fmt.Sprint(ManifestVersion) {
		v.add("version", "必须是 %d", ManifestVersion)
	}
	m.Title = v.str(obj, "title", "", true)
	if utf8.RuneCountInString(m.Title) > maxTitleLength {
		v.add("title", "不能超过 %d 个字符", maxTitleLength)
	}
	switch m.Type = v.str(obj, "type", "", true); m.Type {
	case TypeColumn, TypeVideo, "":
	case TypePaper:
		v.add("type", "论文类型暂不支持上传")
	default:
		v.add("type", "必须是 %q (专栏) 或 %q (视频课)", TypeColumn, TypeVideo)
	}
	switch m.Completion = v.str(obj, "completion", "", false); m.Completion {
	case "":
		m.Completion = Finished
	case Finished, Ongoing, Incomplete:
	default:
		v.add("completion", "必须是 %q (已完结)、%q (连载中) 或 %q (缺失)", Finished, Ongoing, Incomplete)
	}
	m.Intro = v.str(obj, "intro", "", false)
	if a, present := obj["author"]; present && a != nil {
		if author, ok := v.object(a, "author", "name", "intro"); ok {
			m.AuthorName = v.str(author, "name", "author", false)
			m.AuthorIntro = v.str(author, "intro", "author", false)
		}
	}
	if c := v.str(obj, "cover", "", false); c != "" {
		m.Cover = v.filePath(c, "cover")
	}
	m.Chapters = v.chapters(obj["chapters"], m.Type)
	return m
}

func (v *validator) chapters(x any, typ string) []Chapter {
	list, ok := x.([]any)
	if !ok || len(list) == 0 {
		v.add("chapters", "至少需要一章")
		return nil
	}
	chapterKeys := map[string]bool{}
	lessonKeys := map[string]bool{}
	var out []Chapter
	for i, cx := range list {
		at := fmt.Sprintf("chapters[%d]", i)
		obj, ok := v.object(cx, at, "id", "title", "lessons")
		if !ok {
			continue
		}
		ch := Chapter{Key: v.key(obj, at, chapterKeys, "章"), Title: v.str(obj, "title", at, true)}
		lessons, ok := obj["lessons"].([]any)
		if !ok || len(lessons) == 0 {
			v.add(at+".lessons", "每章至少需要一讲")
		}
		for j, lx := range lessons {
			ch.Lessons = append(ch.Lessons, v.lesson(lx, fmt.Sprintf("%s.lessons[%d]", at, j), typ, lessonKeys))
		}
		out = append(out, ch)
	}
	return out
}

func (v *validator) key(obj map[string]any, at string, seen map[string]bool, what string) string {
	k := v.str(obj, "id", at, true)
	if k == "" {
		return ""
	}
	if utf8.RuneCountInString(k) > maxKeyLength {
		v.add(at+".id", "不能超过 %d 个字符", maxKeyLength)
	}
	if seen[k] {
		v.add(at+".id", "%s的 id %q 重复了; 同一门课内每个%s的 id 必须唯一", what, k, what)
	}
	seen[k] = true
	return k
}

func (v *validator) lesson(x any, at, typ string, seen map[string]bool) Lesson {
	obj, ok := v.object(x, at, "id", "title", "file", "video", "duration")
	if !ok {
		return Lesson{}
	}
	l := Lesson{Key: v.key(obj, at, seen, "讲"), Title: v.str(obj, "title", at, true), Duration: v.str(obj, "duration", at, false)}
	if f := v.str(obj, "file", at, false); f != "" {
		l.File = v.filePath(f, at+".file")
	}
	if vx, present := obj["video"]; present && vx != nil {
		if vo, ok := v.object(vx, at+".video", "file", "url"); ok {
			if f := v.str(vo, "file", at+".video", false); f != "" {
				l.VideoFile = v.filePath(f, at+".video.file")
			}
			if u := v.str(vo, "url", at+".video", false); u != "" {
				l.VideoURL = u
				if !isWebURL(u) {
					v.add(at+".video.url", "必须是 http 或 https 链接")
				}
			}
			if (l.VideoFile != "") == (l.VideoURL != "") {
				v.add(at+".video", "file 和 url 必须且只能填一个")
			}
		}
	}
	switch typ {
	case TypeColumn:
		if l.File == "" {
			v.add(at+".file", "专栏的每一讲都需要正文文件")
		}
	case TypeVideo:
		if _, present := obj["video"]; !present {
			v.add(at+".video", "视频课的每一讲都需要视频 (file 或 url)")
		}
	}
	return l
}

// filePath validates a package-relative path and returns its clean form.
func (v *validator) filePath(p, at string) string {
	clean, ok := cleanPath(p)
	if !ok {
		v.add(at, "路径 %q 不合法: 必须是课程包内的相对路径, 不能包含 ..", p)
		return ""
	}
	return clean
}

func cleanPath(p string) (string, bool) {
	p = strings.ReplaceAll(p, "\\", "/")
	if p == "" || strings.HasPrefix(p, "/") {
		return "", false
	}
	c := path.Clean(p)
	if c == "." || c == ".." || strings.HasPrefix(c, "../") || storage.CheckKey(c) != nil {
		return "", false
	}
	return c, true
}

func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
