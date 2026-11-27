package server

import (
	"errors"
	"net/http"

	"github.com/Bank5y/keine/internal/filestore"
	"github.com/Bank5y/keine/internal/storage"
)

// storageView is the object storage settings as the console shows them:
// the secret key only as whether it is set and its last characters.
type storageView struct {
	filestore.Settings
	SealedSecret  []byte               `json:"sealedSecret,omitempty"`
	SecretSet     bool                 `json:"secretSet"`
	SecretFromEnv bool                 `json:"secretFromEnv"`
	MasterKey     bool                 `json:"masterKey"`
	Status        filestore.Status     `json:"status"`
	Migration     *filestore.Migration `json:"migration"`
}

func (s *Server) storageView(st filestore.Settings) storageView {
	fs := s.filestore
	return storageView{
		Settings:      st,
		SecretSet:     len(st.SealedSecret) > 0 || fs.SecretFromEnv(st),
		SecretFromEnv: fs.SecretFromEnv(st),
		MasterKey:     fs.HasMasterKey(),
		Status:        fs.Status(),
		Migration:     fs.LastMigration(),
	}
}

// storageConfigurable answers for a deployment whose files cannot be routed.
func (s *Server) storageConfigurable(w http.ResponseWriter) bool {
	if s.filestore == nil {
		writeError(w, http.StatusConflict, "storage_fixed", "这个部署的文件存储不能在后台配置")
		return false
	}
	return true
}

func (s *Server) getStorage(w http.ResponseWriter, r *http.Request) {
	if !s.storageConfigurable(w) {
		return
	}
	st, err := s.filestore.Load(r.Context())
	if err != nil {
		writeInternal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.storageView(st))
}

func (s *Server) putStorage(w http.ResponseWriter, r *http.Request) {
	if !s.storageConfigurable(w) {
		return
	}
	var u filestore.Update
	if !readJSON(w, r, &u) {
		return
	}
	st, err := s.filestore.Save(r.Context(), u)
	if !writeStorageError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, s.storageView(st))
}

func (s *Server) testStorage(w http.ResponseWriter, r *http.Request) {
	if !s.storageConfigurable(w) {
		return
	}
	var u filestore.Update
	if !readJSON(w, r, &u) {
		return
	}
	res, err := s.filestore.Test(r.Context(), u)
	if !writeStorageError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) startMigration(w http.ResponseWriter, r *http.Request) {
	if !s.storageConfigurable(w) {
		return
	}
	var body struct {
		Direction storage.Direction `json:"direction"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	m, err := s.filestore.Migrate(body.Direction)
	if !writeStorageError(w, r, err) {
		return
	}
	writeJSON(w, http.StatusAccepted, m)
}

func (s *Server) cancelMigration(w http.ResponseWriter, r *http.Request) {
	if !s.storageConfigurable(w) {
		return
	}
	s.filestore.CancelMigration()
	w.WriteHeader(http.StatusNoContent)
}

// writeStorageError answers err and tells whether there was none.
func writeStorageError(w http.ResponseWriter, r *http.Request, err error) bool {
	var invalid filestore.Invalid
	switch {
	case err == nil:
		return true
	case errors.As(err, &invalid):
		writeError(w, http.StatusBadRequest, "invalid", invalid.Message)
	case errors.Is(err, filestore.ErrMasterKeyMissing):
		writeError(w, http.StatusConflict, "master_key_missing", "保存密钥需要先设置 KEINE_MASTER_KEY")
	case errors.Is(err, filestore.ErrNotConnected):
		writeError(w, http.StatusConflict, "not_connected", "对象存储未连接, 请先保存可用的配置")
	case errors.Is(err, filestore.ErrBusy):
		writeError(w, http.StatusConflict, "busy", "已有迁移在进行")
	case errors.Is(err, filestore.ErrConnect):
		writeError(w, http.StatusBadGateway, "unreachable", err.Error())
	default:
		writeInternal(w, r, err)
	}
	return false
}
