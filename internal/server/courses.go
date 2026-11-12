package server

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"

	"github.com/Bank5y/keine/internal/course"
)

const defaultMaxUpload = 4 << 30

// uploadCourse imports a course package sent as the "package" field of a
// multipart form. The upload is streamed to a temporary file, since zip
// needs random access and packages with videos can be large.
func (s *Server) uploadCourse(w http.ResponseWriter, r *http.Request) {
	limit := s.opts.MaxUploadBytes
	if limit <= 0 {
		limit = defaultMaxUpload
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit+1<<20)
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "请以 multipart 表单上传课程包")
		return
	}
	tmp, err := os.CreateTemp("", "keine-upload-*.zip")
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()

	found := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeUploadError(w, r, err)
			return
		}
		if part.FormName() != "package" {
			continue
		}
		if _, err := io.Copy(tmp, io.LimitReader(part, limit+1)); err != nil {
			writeUploadError(w, r, err)
			return
		}
		found = true
		break
	}
	if !found {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少课程包文件")
		return
	}
	size, err := tmp.Seek(0, io.SeekCurrent)
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if size > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "package_too_large", "课程包超过了大小上限")
		return
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		writeProblems(w, http.StatusUnprocessableEntity, "invalid_package", "课程包不是有效的 zip 文件", []course.Problem{})
		return
	}
	id, err := s.courses.Import(r.Context(), currentUser(r), zr)
	var invalid *course.InvalidPackage
	if errors.As(err, &invalid) {
		writeProblems(w, http.StatusUnprocessableEntity, "invalid_package", "课程包有问题, 请修正后重新上传", invalid.Problems)
		return
	}
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func writeUploadError(w http.ResponseWriter, r *http.Request, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "package_too_large", "课程包超过了大小上限")
		return
	}
	writeError(w, http.StatusBadRequest, "upload_failed", "上传中断: "+err.Error())
}

func (s *Server) getCourse(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	d, err := s.courses.Get(r.Context(), currentUser(r), id)
	if !writeCourseError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) myCourses(w http.ResponseWriter, r *http.Request) {
	list, err := s.courses.Mine(r.Context(), currentUser(r))
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	if list == nil {
		list = []course.Summary{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"courses": list})
}

// writeCourseError answers the course errors shared by several endpoints and
// reports whether err was nil.
func writeCourseError(w http.ResponseWriter, r *http.Request, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, course.ErrNotFound):
		writeError(w, http.StatusNotFound, "course_not_found", "课程不存在")
	case errors.Is(err, course.ErrLessonNotFound):
		writeError(w, http.StatusNotFound, "lesson_not_found", "这一讲不存在")
	case errors.Is(err, course.ErrFileNotFound):
		writeError(w, http.StatusNotFound, "file_not_found", "文件不存在")
	case errors.Is(err, course.ErrBadProgress):
		writeError(w, http.StatusBadRequest, "bad_request", "学习进度不合法")
	case errors.Is(err, course.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "没有权限进行这个操作")
	default:
		writeInternal(w, r, err)
	}
	return false
}

func (s *Server) courseCover(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	rc, name, err := s.courses.Cover(r.Context(), currentUser(r), id)
	if errors.Is(err, course.ErrNoCover) {
		writeError(w, http.StatusNotFound, "cover_not_found", "课程没有封面")
		return
	}
	if !writeCourseError(w, r, err) {
		return
	}
	defer rc.Close()
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	setUserFileHeaders(w)
	w.Header().Set("Cache-Control", "private, max-age=300")
	_, _ = io.Copy(w, rc)
}

func (s *Server) deleteCourse(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	if !writeCourseError(w, r, s.courses.Delete(r.Context(), currentUser(r), id)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) manifestPrompt(w http.ResponseWriter, r *http.Request) {
	a, err := s.generator.Help(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": a.Filename}))
	_, _ = w.Write(a.Body)
}

// setUserFileHeaders guards responses that carry files uploaded by users:
// they are served from our origin, so they must never run as a page (an SVG
// or HTML file could carry scripts).
func setUserFileHeaders(w http.ResponseWriter) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; script-src 'none'; sandbox")
}

// lessonBody is a lesson for the reader. Base is the URL folder that the
// relative links in its Markdown resolve against.
type lessonBody struct {
	course.LessonText
	Base string `json:"base"`
}

// lessonParams reads the {id} and {lesson} path segments.
func lessonParams(w http.ResponseWriter, r *http.Request) (courseID, lessonID int64, ok bool) {
	if courseID, ok = courseIDParam(w, r); !ok {
		return 0, 0, false
	}
	lessonID, err := strconv.ParseInt(r.PathValue("lesson"), 10, 64)
	if err != nil || lessonID <= 0 {
		writeError(w, http.StatusNotFound, "lesson_not_found", "这一讲不存在")
		return 0, 0, false
	}
	return courseID, lessonID, true
}

func (s *Server) visitLesson(w http.ResponseWriter, r *http.Request) {
	id, lessonID, ok := lessonParams(w, r)
	if !ok || !writeCourseError(w, r, s.courses.Visit(r.Context(), currentUser(r), id, lessonID)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) setProgress(w http.ResponseWriter, r *http.Request) {
	id, lessonID, ok := lessonParams(w, r)
	if !ok {
		return
	}
	var in course.ProgressUpdate
	if !readJSON(w, r, &in) {
		return
	}
	if !writeCourseError(w, r, s.courses.SetProgress(r.Context(), currentUser(r), id, lessonID, in)) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getLesson(w http.ResponseWriter, r *http.Request) {
	id, lessonID, ok := lessonParams(w, r)
	if !ok {
		return
	}
	l, err := s.courses.Lesson(r.Context(), currentUser(r), id, lessonID)
	if !writeCourseError(w, r, err) {
		return
	}
	base := fmt.Sprintf("/api/courses/%d/files/", id)
	if dir := path.Dir(l.File); l.File != "" && dir != "." {
		base += (&url.URL{Path: dir}).EscapedPath() + "/"
	}
	writeJSON(w, http.StatusOK, lessonBody{LessonText: l, Base: base})
}

func (s *Server) courseFile(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	name := r.PathValue("path")
	rc, err := s.courses.File(r.Context(), currentUser(r), id, name)
	if !writeCourseError(w, r, err) {
		return
	}
	defer rc.Close()
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	setUserFileHeaders(w)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = io.Copy(w, rc)
}
