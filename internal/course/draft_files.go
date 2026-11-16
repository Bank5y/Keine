package course

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Bank5y/keine/internal/auth"
	"github.com/Bank5y/keine/internal/storage"
)

// A draft's temporary area (ADR-0006) holds the lesson files uploaded in
// the editor until the draft is committed, discarded or expires:
//
//	courses/{course}/d{draft}/{upload}/{path}
//
// One upload holds the files of one lesson's content; an operation names
// it. The area lives under the course, so purging a course removes it, and
// it is removed as a whole with its draft. In the draft's course, and in
// the version it commits to, the files keep their place as "{upload}/{path}".

// ErrUploadNotFound means the upload is not one of the viewer's draft.
var ErrUploadNotFound = errors.New("draft upload not found")

// draftPrefix is the temporary area of a draft.
func draftPrefix(courseID, draftID int64) string {
	return fmt.Sprintf("courses/%d/d%d/", courseID, draftID)
}

var uploadName = regexp.MustCompile(`^u-[0-9a-f]{16}$`)

// LessonContent is the content an operation gives a lesson, from one
// upload: its text (a Markdown file of the upload), its video (a video
// file of the upload, or a link), or both. Left out, the text and video
// file are found in the upload when it holds just one of each.
type LessonContent struct {
	Upload    string `json:"upload,omitempty"`
	Text      string `json:"text,omitempty"`
	VideoFile string `json:"videoFile,omitempty"`
	VideoURL  string `json:"videoUrl,omitempty"`
}

// lesson gives l the content, with paths as the draft's course has them.
func (c *LessonContent) lesson(typ string, l Lesson) (Lesson, error) {
	l.File, l.VideoFile, l.VideoURL, l.Duration = "", "", c.VideoURL, ""
	if c.Text != "" {
		l.File = c.Upload + "/" + c.Text
	}
	if c.VideoFile != "" {
		l.VideoFile = c.Upload + "/" + c.VideoFile
	}
	switch {
	case l.VideoFile != "" && l.VideoURL != "":
		return l, reject("视频文件和视频链接只能有一个")
	case typ == TypeColumn && l.File == "":
		return l, reject("专栏的每一讲都需要正文 (.md 文件)")
	case typ == TypeVideo && l.VideoFile == "" && l.VideoURL == "":
		return l, reject("视频课的每一讲都需要视频文件或视频链接")
	case l.Upcoming():
		return l, reject("这一讲还没有内容")
	}
	return l, nil
}

// draftRow is a user's draft of a course as stored.
type draftRow struct {
	ID  int64 // 0: no draft
	Ops []Op
}

func loadDraft(ctx context.Context, q querier, courseID, userID int64, lock bool) (draftRow, error) {
	sql := "SELECT id, ops FROM course_drafts WHERE course_id = $1 AND user_id = $2"
	if lock {
		sql += " FOR UPDATE"
	}
	var d draftRow
	var raw []byte
	err := q.QueryRow(ctx, sql, courseID, userID).Scan(&d.ID, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return draftRow{Ops: []Op{}}, nil
	}
	if err != nil {
		return d, err
	}
	d.Ops = []Op{}
	return d, json.Unmarshal(raw, &d.Ops)
}

// uploads are the uploads the draft's operations use.
func (d draftRow) uploads() map[string]bool {
	out := map[string]bool{}
	for _, op := range d.Ops {
		if op.Content != nil && op.Content.Upload != "" {
			out[op.Content.Upload] = true
		}
	}
	return out
}

// draftFiles finds the files of a draft's course: those of its uploads in
// the temporary area, the others in the course's current folder.
type draftFiles struct {
	courseID int64
	dir      string // the course's current folder
	draftID  int64
	uploads  map[string]bool
}

func (f draftFiles) key(name string) string {
	if up, _, ok := strings.Cut(name, "/"); ok && f.uploads[up] {
		return draftPrefix(f.courseID, f.draftID) + name
	}
	return filesPrefix(f.courseID, f.dir) + name
}

// StartDraftUpload starts an upload for one lesson's content in the
// viewer's draft, starting the draft if they have none.
func (s *Service) StartDraftUpload(ctx context.Context, viewer auth.User, id int64) (string, error) {
	sm, _, err := s.loadToEdit(ctx, viewer, id)
	if err != nil {
		return "", err
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO course_drafts (course_id, user_id, base_dir, updated_at) VALUES ($1, $2, $3, $4)
		ON CONFLICT (course_id, user_id) DO UPDATE SET updated_at = EXCLUDED.updated_at`, id, viewer.ID, sm.FilesDir, s.now())
	return newKey("u"), err
}

// uploadPrefix checks that upload can belong to the viewer's draft and
// returns where its files are kept.
func (s *Service) uploadPrefix(ctx context.Context, viewer auth.User, id int64, upload string) (string, error) {
	if _, _, err := s.loadToEdit(ctx, viewer, id); err != nil {
		return "", err
	}
	if !uploadName.MatchString(upload) {
		return "", ErrUploadNotFound
	}
	d, err := loadDraft(ctx, s.db, id, viewer.ID, false)
	if err != nil {
		return "", err
	}
	if d.ID == 0 {
		return "", ErrUploadNotFound
	}
	return draftPrefix(id, d.ID) + upload + "/", nil
}

// PutDraftFile stores one file of an upload under its path in the upload.
// Sending a file again replaces it.
func (s *Service) PutDraftFile(ctx context.Context, viewer auth.User, id int64, upload, name string, r io.Reader, size int64) error {
	prefix, err := s.uploadPrefix(ctx, viewer, id, upload)
	if err != nil {
		return err
	}
	clean, ok := cleanPath(name)
	if !ok || clean != name {
		return reject("文件路径 %q 不合法", name)
	}
	return s.files.Put(ctx, prefix+clean, r, size)
}

// PutDraftZip stores the files of a single lesson's zip in an upload. A
// zip of one folder counts from inside that folder.
func (s *Service) PutDraftZip(ctx context.Context, viewer auth.User, id int64, upload string, zr *zip.Reader) error {
	prefix, err := s.uploadPrefix(ctx, viewer, id, upload)
	if err != nil {
		return err
	}
	files := map[string]*zip.File{}
	tops := map[string]bool{}
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if f.FileInfo().IsDir() || strings.HasPrefix(name, "__MACOSX/") {
			continue
		}
		files[name] = f
		top, _, nested := strings.Cut(name, "/")
		if !nested {
			top = ""
		}
		tops[top] = true
	}
	root := ""
	if len(tops) == 1 {
		for t := range tops {
			if t != "" {
				root = t + "/"
			}
		}
	}
	for name, f := range files {
		clean, ok := cleanPath(strings.TrimPrefix(name, root))
		if !ok {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return reject("无法读取 zip 里的 %s", name)
		}
		err = s.files.Put(ctx, prefix+clean, rc, int64(f.UncompressedSize64))
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// prepareContent checks an operation's content against its upload: the
// text and video are found, and every image the text references is there.
// A picked file arrives without its folder, so an image missing at its
// path is taken from a file of the same name. Missing images are listed.
func (s *Service) prepareContent(ctx context.Context, courseID int64, d draftRow, c *LessonContent) error {
	c.Text, c.VideoFile, c.VideoURL = strings.TrimSpace(c.Text), strings.TrimSpace(c.VideoFile), strings.TrimSpace(c.VideoURL)
	if c.VideoURL != "" && !isWebURL(c.VideoURL) {
		return reject("视频链接必须是 http 或 https 链接")
	}
	if c.Upload == "" {
		if c.Text != "" || c.VideoFile != "" {
			return ErrUploadNotFound
		}
		return nil
	}
	if !uploadName.MatchString(c.Upload) || d.ID == 0 {
		return ErrUploadNotFound
	}
	prefix := draftPrefix(courseID, d.ID) + c.Upload + "/"
	keys, err := s.files.List(ctx, prefix)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	var names []string
	for _, k := range keys {
		names = append(names, strings.TrimPrefix(k, prefix))
		have[names[len(names)-1]] = true
	}
	if len(names) == 0 && c.VideoURL == "" {
		return reject("还没有上传这一讲的文件")
	}
	pick := func(given *string, what string, match func(string) bool) error {
		if *given != "" {
			if !have[*given] {
				return reject("上传的文件里没有 %s", *given)
			}
			return nil
		}
		var found []string
		for _, n := range names {
			if match(n) {
				found = append(found, n)
			}
		}
		if len(found) > 1 {
			return reject("上传的文件里有多个%s: %s; 一讲只能有一个", what, strings.Join(found, ", "))
		}
		if len(found) == 1 {
			*given = found[0]
		}
		return nil
	}
	if err := pick(&c.Text, "正文 (.md)", func(n string) bool { return strings.EqualFold(path.Ext(n), ".md") }); err != nil {
		return err
	}
	if c.VideoURL == "" {
		if err := pick(&c.VideoFile, "视频", IsVideoFile); err != nil {
			return err
		}
	}
	if c.Text == "" {
		return nil
	}
	b, err := s.readFile(ctx, prefix+c.Text)
	if err != nil {
		return err
	}
	var problems []Problem
	for _, img := range localImages(string(b)) {
		target, ok := resolveRef(c.Text, img)
		if !ok {
			problems = append(problems, Problem{c.Text, fmt.Sprintf("图片路径 %q 超出了这一讲的文件", img)})
			continue
		}
		if have[target] {
			continue
		}
		var same []string
		for _, n := range names {
			if path.Base(n) == path.Base(target) {
				same = append(same, n)
			}
		}
		if len(same) != 1 {
			problems = append(problems, Problem{c.Text, "引用的图片不存在: " + target})
			continue
		}
		if err := s.files.Copy(ctx, prefix+same[0], prefix+target); err != nil {
			return err
		}
		have[target] = true
	}
	if len(problems) > 0 {
		return &OpRejected{Message: fmt.Sprintf("正文引用的图片缺了 %d 张, 请补上后再试", len(problems)), Problems: problems}
	}
	return nil
}

// IsVideoFile tells whether a file name is a video, by its extension.
func IsVideoFile(name string) bool { return directMedia[strings.ToLower(path.Ext(name))] }

// DraftLessonText is a lesson of the viewer's draft as the reader would
// show it.
type DraftLessonText struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Markdown  string `json:"markdown"`
	File      string `json:"-"`
	VideoFile string `json:"-"`
	VideoURL  string `json:"-"`
}

// draftCourse applies the viewer's draft to the course's latest version.
func (s *Service) draftCourse(ctx context.Context, viewer auth.User, id int64) (Manifest, draftFiles, error) {
	if _, _, err := s.loadToEdit(ctx, viewer, id); err != nil {
		return Manifest{}, draftFiles{}, err
	}
	d, err := loadDraft(ctx, s.db, id, viewer.ID, false)
	if err != nil {
		return Manifest{}, draftFiles{}, err
	}
	cur, dir, err := currentManifest(ctx, s.db, id)
	if err != nil {
		return Manifest{}, draftFiles{}, err
	}
	m, err := applyOps(cur, d.Ops)
	return m, draftFiles{courseID: id, dir: dir, draftID: d.ID, uploads: d.uploads()}, err
}

// DraftLesson previews a lesson of the viewer's draft, named by key.
func (s *Service) DraftLesson(ctx context.Context, viewer auth.User, id int64, key string) (DraftLessonText, error) {
	m, files, err := s.draftCourse(ctx, viewer, id)
	if err != nil {
		return DraftLessonText{}, err
	}
	ci, li, ok := m.lesson(key)
	if !ok {
		return DraftLessonText{}, ErrLessonNotFound
	}
	l := m.Chapters[ci].Lessons[li]
	if l.Upcoming() {
		return DraftLessonText{}, ErrLessonUpcoming
	}
	out := DraftLessonText{Key: l.Key, Title: l.Title, File: l.File, VideoFile: l.VideoFile, VideoURL: l.VideoURL}
	if l.File != "" {
		b, err := s.readFile(ctx, files.key(l.File))
		if err != nil && !errors.Is(err, storage.ErrNotFound) {
			return out, err
		}
		out.Markdown = string(b)
	}
	return out, nil
}

// DraftFile streams a file of the viewer's draft's course: an uploaded one
// from the temporary area, or one of the course's current files.
func (s *Service) DraftFile(ctx context.Context, viewer auth.User, id int64, name string) (io.ReadCloser, error) {
	_, files, err := s.draftCourse(ctx, viewer, id)
	if err != nil {
		return nil, err
	}
	if clean, ok := cleanPath(name); !ok || clean != name {
		return nil, ErrFileNotFound
	}
	rc, err := s.files.Get(ctx, files.key(name))
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrInvalidKey) {
		return nil, ErrFileNotFound
	}
	return rc, err
}

// dropDraftFiles removes a draft's temporary area, or one upload of it.
// Leftovers only cost space, so a failure is logged.
func (s *Service) dropDraftFiles(ctx context.Context, courseID, draftID int64, upload string) {
	if draftID == 0 {
		return
	}
	prefix := draftPrefix(courseID, draftID)
	if upload != "" {
		prefix += upload + "/"
	}
	if err := s.files.DeletePrefix(context.WithoutCancel(ctx), prefix); err != nil {
		slog.Warn("draft files not removed", "course", courseID, "draft", draftID, "upload", upload, "err", err)
	}
}

// dropDrafts deletes every draft of a course that stays in the database
// though nobody edits it any more (a deleted one), returning their ids for
// dropDraftFiles once the transaction commits. A purged course needs none:
// its drafts and their areas go with it.
func dropDrafts(ctx context.Context, tx pgx.Tx, courseID int64) ([]int64, error) {
	rows, err := tx.Query(ctx, "DELETE FROM course_drafts WHERE course_id = $1 RETURNING id", courseID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

// unusedUpload is the upload of a dropped operation once no other
// operation of the draft uses it; "" otherwise.
func unusedUpload(dropped Op, rest []Op) string {
	if dropped.Content == nil || dropped.Content.Upload == "" {
		return ""
	}
	up := dropped.Content.Upload
	if slices.ContainsFunc(rest, func(op Op) bool { return op.Content != nil && op.Content.Upload == up }) {
		return ""
	}
	return up
}
