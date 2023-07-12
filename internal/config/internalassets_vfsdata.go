// Code generated by vfsgen; DO NOT EDIT.

package config

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	pathpkg "path"
	"time"
)

// internalAssets statically implements the virtual filesystem provided to vfsgen.
var internalAssets = func() http.FileSystem {
	fs := vfsgen۰FS{
		"/": &vfsgen۰DirInfo{
			name:    "/",
			modTime: time.Date(2023, 7, 12, 11, 57, 26, 276740445, time.UTC),
		},
		"/zsys.conf": &vfsgen۰CompressedFileInfo{
			name:             "zsys.conf",
			modTime:          time.Date(2023, 7, 11, 23, 21, 46, 604704670, time.UTC),
			uncompressedSize: 908,

			compressedContent: []byte("\x1f\x8b\x08\x00\x00\x00\x00\x00\x02\xff\x94\x93\x4d\xab\xdb\x3c\x10\x85\xf7\xfe\x15\x07\xee\xe6\x7d\x17\x2d\x37\xfd\x04\xef\x0a\x77\xd7\xde\xd2\x45\xa1\xeb\x89\x7d\x1c\x8b\x58\x23\x77\x34\x0e\xf8\xdf\x17\x29\x4e\x6f\x48\x43\xa1\x5e\x59\x33\x3a\xcf\x7c\x6a\x0c\xd9\x93\xad\x6d\x03\x3c\xe0\x33\x39\x43\x1c\x13\x25\x3b\x14\x9b\x13\x54\xb7\x15\x33\x0d\x8b\x06\x47\x1a\xe0\x21\x12\x61\x00\x35\x2d\x87\xb1\x5a\x46\x46\x88\x11\xb3\x31\x53\xbd\x02\xbf\x8f\x44\xb2\x9e\x86\x2e\x69\x1f\x3c\x24\x2d\x17\xb1\x5f\xba\x23\x1d\xd9\xc5\x1c\xa2\x3d\xa8\x3d\x7a\x71\x66\xfc\x37\x58\x8a\x88\x29\x3b\x8c\x1d\xd5\xe1\x09\x69\xea\x99\xfd\xff\x06\x38\x74\x55\x24\x83\xd3\x5a\xec\x1a\xe0\x48\xce\x93\x64\x6f\xf1\xe6\x11\x0f\x78\x0e\x1a\xe2\x12\xa1\x4b\xdc\xd3\x4a\x66\x1b\x26\x7b\xe5\x7b\xaa\x8a\xd7\x35\x3f\x00\xaf\xa0\x12\xd9\xe2\xfa\xfb\xb4\x0f\x6e\x62\x6b\x75\x6d\xc5\x6d\x39\x5f\x64\xd8\xce\xf9\x4a\xf9\xf5\x77\xc8\xcd\x87\x74\xa2\x55\x71\x50\xa7\x9d\x64\xba\x95\x4f\xd4\x83\x8f\x67\xc6\x97\xfa\x5f\xe4\x94\x6e\xbc\xf4\x28\x28\x7a\x59\xf3\x8b\x30\x4b\x9c\x27\xe6\x99\x76\xbe\xd1\x5e\xc5\xed\xc5\x25\x97\xc0\x5b\x95\x45\x7d\x05\xab\xfd\xb3\x65\x62\x2e\xf3\x7e\xa9\xfd\x9b\xf1\x14\xd2\x92\x9f\x64\x6d\x6e\x8a\xdb\x35\xf7\xd2\xbd\x58\xff\xcc\xe5\xed\x5d\xf0\x0f\xf2\x78\x4b\x7e\xff\x8f\xe4\xdd\x5d\xf2\x73\x52\x1f\x6f\xd1\xef\xee\xa2\x3f\xfe\x05\x7d\xa0\xd2\x64\x3a\x3f\x83\xba\x42\x32\x61\x30\x12\x79\x96\x8e\x30\xfe\x5c\x82\xb1\xc7\x9e\x43\x32\xc2\xe5\x18\xf4\x00\x41\x56\x99\xf3\x98\x4a\x6b\x63\xd0\xa2\x98\x53\x9a\xaa\xa8\x2c\x64\xe5\x3d\x09\x63\x59\xfc\x10\x99\x96\x3a\xd1\xcc\xf2\x1e\xca\x50\x37\x63\x8b\x0f\x8f\xcd\xaf\x00\x00\x00\xff\xff\xbf\xda\xe7\x4a\x8c\x03\x00\x00"),
		},
	}
	fs["/"].(*vfsgen۰DirInfo).entries = []os.FileInfo{
		fs["/zsys.conf"].(os.FileInfo),
	}

	return fs
}()

type vfsgen۰FS map[string]interface{}

func (fs vfsgen۰FS) Open(path string) (http.File, error) {
	path = pathpkg.Clean("/" + path)
	f, ok := fs[path]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	switch f := f.(type) {
	case *vfsgen۰CompressedFileInfo:
		gr, err := gzip.NewReader(bytes.NewReader(f.compressedContent))
		if err != nil {
			// This should never happen because we generate the gzip bytes such that they are always valid.
			panic("unexpected error reading own gzip compressed bytes: " + err.Error())
		}
		return &vfsgen۰CompressedFile{
			vfsgen۰CompressedFileInfo: f,
			gr:                        gr,
		}, nil
	case *vfsgen۰DirInfo:
		return &vfsgen۰Dir{
			vfsgen۰DirInfo: f,
		}, nil
	default:
		// This should never happen because we generate only the above types.
		panic(fmt.Sprintf("unexpected type %T", f))
	}
}

// vfsgen۰CompressedFileInfo is a static definition of a gzip compressed file.
type vfsgen۰CompressedFileInfo struct {
	name              string
	modTime           time.Time
	compressedContent []byte
	uncompressedSize  int64
}

func (f *vfsgen۰CompressedFileInfo) Readdir(count int) ([]os.FileInfo, error) {
	return nil, fmt.Errorf("cannot Readdir from file %s", f.name)
}
func (f *vfsgen۰CompressedFileInfo) Stat() (os.FileInfo, error) { return f, nil }

func (f *vfsgen۰CompressedFileInfo) GzipBytes() []byte {
	return f.compressedContent
}

func (f *vfsgen۰CompressedFileInfo) Name() string       { return f.name }
func (f *vfsgen۰CompressedFileInfo) Size() int64        { return f.uncompressedSize }
func (f *vfsgen۰CompressedFileInfo) Mode() os.FileMode  { return 0444 }
func (f *vfsgen۰CompressedFileInfo) ModTime() time.Time { return f.modTime }
func (f *vfsgen۰CompressedFileInfo) IsDir() bool        { return false }
func (f *vfsgen۰CompressedFileInfo) Sys() interface{}   { return nil }

// vfsgen۰CompressedFile is an opened compressedFile instance.
type vfsgen۰CompressedFile struct {
	*vfsgen۰CompressedFileInfo
	gr      *gzip.Reader
	grPos   int64 // Actual gr uncompressed position.
	seekPos int64 // Seek uncompressed position.
}

func (f *vfsgen۰CompressedFile) Read(p []byte) (n int, err error) {
	if f.grPos > f.seekPos {
		// Rewind to beginning.
		err = f.gr.Reset(bytes.NewReader(f.compressedContent))
		if err != nil {
			return 0, err
		}
		f.grPos = 0
	}
	if f.grPos < f.seekPos {
		// Fast-forward.
		_, err = io.CopyN(io.Discard, f.gr, f.seekPos-f.grPos)
		if err != nil {
			return 0, err
		}
		f.grPos = f.seekPos
	}
	n, err = f.gr.Read(p)
	f.grPos += int64(n)
	f.seekPos = f.grPos
	return n, err
}
func (f *vfsgen۰CompressedFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		f.seekPos = 0 + offset
	case io.SeekCurrent:
		f.seekPos += offset
	case io.SeekEnd:
		f.seekPos = f.uncompressedSize + offset
	default:
		panic(fmt.Errorf("invalid whence value: %v", whence))
	}
	return f.seekPos, nil
}
func (f *vfsgen۰CompressedFile) Close() error {
	return f.gr.Close()
}

// vfsgen۰DirInfo is a static definition of a directory.
type vfsgen۰DirInfo struct {
	name    string
	modTime time.Time
	entries []os.FileInfo
}

func (d *vfsgen۰DirInfo) Read([]byte) (int, error) {
	return 0, fmt.Errorf("cannot Read from directory %s", d.name)
}
func (d *vfsgen۰DirInfo) Close() error               { return nil }
func (d *vfsgen۰DirInfo) Stat() (os.FileInfo, error) { return d, nil }

func (d *vfsgen۰DirInfo) Name() string       { return d.name }
func (d *vfsgen۰DirInfo) Size() int64        { return 0 }
func (d *vfsgen۰DirInfo) Mode() os.FileMode  { return 0755 | os.ModeDir }
func (d *vfsgen۰DirInfo) ModTime() time.Time { return d.modTime }
func (d *vfsgen۰DirInfo) IsDir() bool        { return true }
func (d *vfsgen۰DirInfo) Sys() interface{}   { return nil }

// vfsgen۰Dir is an opened dir instance.
type vfsgen۰Dir struct {
	*vfsgen۰DirInfo
	pos int // Position within entries for Seek and Readdir.
}

func (d *vfsgen۰Dir) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekStart {
		d.pos = 0
		return 0, nil
	}
	return 0, fmt.Errorf("unsupported Seek in directory %s", d.name)
}

func (d *vfsgen۰Dir) Readdir(count int) ([]os.FileInfo, error) {
	if d.pos >= len(d.entries) && count > 0 {
		return nil, io.EOF
	}
	if count <= 0 || count > len(d.entries)-d.pos {
		count = len(d.entries) - d.pos
	}
	e := d.entries[d.pos : d.pos+count]
	d.pos += count
	return e, nil
}
