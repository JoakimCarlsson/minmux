package router

import "mime/multipart"

// FormFile represents a single file uploaded as part of a multipart form.
//
// It is an alias for [multipart.FileHeader] so handlers can use the stdlib
// API directly: call Open to obtain a [multipart.File] reader, and consult
// Filename, Size, and the MIME Header for metadata.
//
//	type UploadParams struct {
//	    Avatar *router.FormFile   `file:"avatar"`
//	    Photos []*router.FormFile `file:"photos"`
//	}
//
//	func upload(c *router.Context, p UploadParams) {
//	    f, err := p.Avatar.Open()
//	    if err != nil { /* ... */ }
//	    defer f.Close()
//	    // p.Avatar.Filename, p.Avatar.Size, p.Avatar.Header.Get("Content-Type")
//	}
type FormFile = multipart.FileHeader
