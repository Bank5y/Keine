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
	"strings"
	"time"

	"github.com/Bank5y/keine/internal/course"
)

// Lesson content uploaded to a course draft: each lesson's files go to an
// upload of the draft, one request per file so a failed file is sent again
// on its own; an operation then gives a lesson the upload's content.

func (s *Server) startDraftUpload(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	upload, err := s.courses.StartDraftUpload(r.Context(), currentUser(r), id)
	if writeDraftError(w, r, err) {
		writeJSON(w, http.StatusCreated, map[string]any{"upload": upload})
	}
}

// putDraftFile stores the request body as one file of an upload. A .zip is
// a single lesson's zip: its files are stored instead.
func (s *Server) putDraftFile(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	limit := s.opts.MaxUploadBytes
	if limit <= 0 {
		limit = defaultMaxUpload
	}
	if r.ContentLength < 0 {
		writeError(w, http.StatusLengthRequired, "length_required", "上传请求缺少文件大小")
		return
	}
	if r.ContentLength > limit {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "文件超过了大小上限")
		return
	}
	body := http.MaxBytesReader(w, r.Body, r.ContentLength)
	upload, name := r.PathValue("upload"), r.PathValue("path")
	var err error
	if strings.EqualFold(path.Ext(name), ".zip") {
		err = s.putDraftZip(r, id, upload, body)
	} else {
		err = s.courses.PutDraftFile(r.Context(), currentUser(r), id, upload, name, body, r.ContentLength)
	}
	if writeDraftError(w, r, err) {
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *Server) putDraftZip(r *http.Request, id int64, upload string, body io.Reader) error {
	tmp, err := os.CreateTemp("", "keine-lesson-*.zip")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	size, err := io.Copy(tmp, body)
	if err != nil {
		return uploadFailure(err)
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return &course.OpRejected{Message: "不是有效的 zip 文件"}
	}
	// unpacked, a lesson's zip may hold no more than a file sent as is
	limit := s.opts.MaxUploadBytes
	if limit <= 0 {
		limit = defaultMaxUpload
	}
	return s.courses.PutDraftZip(r.Context(), currentUser(r), id, upload, zr, limit)
}

// draftFilesURL is where a file of the viewer's draft is served.
func draftFilesURL(courseID int64, file string) string {
	return fmt.Sprintf("/api/courses/%d/draft/files/", courseID) + (&url.URL{Path: file}).EscapedPath()
}

// draftLessonBody is a lesson of a draft as the reader shows it.
type draftLessonBody struct {
	course.DraftLessonText
	Base  string     `json:"base"`
	Video *videoBody `json:"video"`
}

// previewDraftLesson shows a lesson of the viewer's draft, with its
// uploaded text, images and video, before the draft is committed.
func (s *Server) previewDraftLesson(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	l, err := s.courses.DraftLesson(r.Context(), currentUser(r), id, r.PathValue("lesson"))
	if !writeDraftError(w, r, err) {
		return
	}
	body := draftLessonBody{DraftLessonText: l, Base: draftFilesURL(id, "")}
	if dir := path.Dir(l.File); l.File != "" && dir != "." {
		body.Base = draftFilesURL(id, dir) + "/"
	}
	switch {
	case l.VideoFile != "":
		body.Video = &videoBody{Mode: course.VideoPlayer, Src: draftFilesURL(id, l.VideoFile)}
	case l.VideoURL != "":
		mode, src := course.ClassifyVideoURL(l.VideoURL)
		body.Video = &videoBody{Mode: mode, Src: src}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) draftFile(w http.ResponseWriter, r *http.Request) {
	id, ok := courseIDParam(w, r)
	if !ok {
		return
	}
	name := r.PathValue("path")
	rc, err := s.courses.DraftFile(r.Context(), currentUser(r), id, name)
	if !writeDraftError(w, r, err) {
		return
	}
	defer rc.Close()
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	setUserFileHeaders(w)
	w.Header().Set("Cache-Control", "private, no-cache")
	if rs, ok := rc.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, time.Time{}, rs)
		return
	}
	_, _ = io.Copy(w, rc)
}

func isUploadNotFound(err error) bool { return errors.Is(err, course.ErrUploadNotFound) }
