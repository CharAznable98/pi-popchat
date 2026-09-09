package core

import (
	"encoding/base64"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const MaxAttachmentBytes = 32 * 1024 * 1024
const MaxMessageImageBytes = 16 * 1024 * 1024

func (e *Engine) SaveAttachment(name, mimeType, encoded string) (Attachment, error) {
	if len(encoded) > MaxAttachmentBytes*4/3+8 {
		return Attachment{}, errors.New("单个附件不能超过 32 MB")
	}
	b, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Attachment{}, errors.New("附件编码无效")
	}
	if len(b) > MaxAttachmentBytes {
		return Attachment{}, errors.New("单个附件不能超过 32 MB")
	}
	name = filepath.Base(name)
	if name == "." || name == "/" || name == "" {
		name = "attachment"
	}
	aid := id()
	dir := filepath.Join(e.store.Root, "attachments", aid)
	if err = os.MkdirAll(dir, 0700); err != nil {
		return Attachment{}, err
	}
	path := filepath.Join(dir, name)
	if err = os.WriteFile(path, b, 0600); err != nil {
		return Attachment{}, err
	}
	detected := http.DetectContentType(b)
	if strings.HasPrefix(detected, "image/") {
		mimeType = detected
	} else if strings.HasPrefix(mimeType, "image/") {
		mimeType = "application/octet-stream"
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(filepath.Ext(name))
		if mimeType == "" {
			mimeType = detected
		}
	}
	a := Attachment{ID: aid, Name: name, Path: path, MIME: mimeType}
	if mimeType == "image/png" || mimeType == "image/jpeg" || mimeType == "image/gif" || mimeType == "image/webp" {
		a.Preview = "data:" + mimeType + ";base64," + encoded
	}
	return a, nil
}
func (e *Engine) validateAttachment(a Attachment) error {
	root, rootErr := filepath.EvalSymlinks(filepath.Join(e.store.Root, "attachments"))
	if rootErr != nil {
		return errors.New("附件目录不存在")
	}
	p, err := filepath.EvalSymlinks(a.Path)
	if err != nil {
		return errors.New("附件已不存在，请重新添加")
	}
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("附件路径不属于应用管理目录")
	}
	info, err := os.Stat(p)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxAttachmentBytes {
		return errors.New("附件无效或超过 32 MB")
	}
	return nil
}
