package gar

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const suffix = "GAR"

type Archiver struct {
	f        *os.File
	fw       *bufio.Writer
	fileSize int64
	zw       *zip.Writer
	err      error
}

func NewArchiver(fn string) (*Archiver, error) {
	f, err := os.OpenFile(fn, os.O_WRONLY|os.O_APPEND, 0755)
	if err != nil {
		return nil, err
	}
	fw := bufio.NewWriter(f)
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &Archiver{
		f:        f,
		fw:       fw,
		fileSize: fi.Size(),
		zw:       zip.NewWriter(fw),
	}, nil
}

func (ar *Archiver) Add(fn string) error {
	if ar.err != nil {
		return ar.err
	}

	in, err := os.Open(fn)
	if err != nil {
		return err
	}
	defer in.Close()

	fn = filepath.Clean(fn)
	fn = filepath.ToSlash(fn)
	fn = strings.TrimLeft(fn, "/") // make it relative to cwd

	out, err := ar.zw.Create(fn)
	if ar.setErr(err) != nil {
		return ar.err
	}
	_, err = io.Copy(out, in)
	return ar.setErr(err)
}

func (ar *Archiver) setErr(err error) error {
	if ar.err == nil {
		ar.err = err
	}
	return ar.err
}

func (ar *Archiver) Close() error {
	if ar.f == nil {
		return ar.err
	}
	ar.setErr(ar.zw.Close())
	if ar.err == nil {
		b := make([]byte, 8+len(suffix))
		binary.BigEndian.PutUint64(b, uint64(ar.fileSize))
		copy(b[8:], suffix)
		_, err := ar.fw.Write(b)
		ar.setErr(err)
	}
	if ar.err == nil {
		ar.setErr(ar.fw.Flush())
	}
	fn := ar.f.Name()
	ar.setErr(ar.f.Close())

	if ar.err != nil {
		// If anything goes wrong, restore the file
		if err := os.Truncate(fn, ar.fileSize); err != nil {
			// The file is corrupted probably. No way to report the error other than panic.
			panic(err)
		}
	}

	err := ar.err
	*ar = Archiver{err: io.ErrClosedPipe} // zero out to mean it is closed
	return err
}

type FileInfo struct {
	Name string
	Size int64
}

type File struct {
	FileInfo
	io.ReadCloser
}

type FileSystem struct {
	BinarySize int64
	f          *os.File
	zr         *zip.Reader
	files      map[string]*zip.File
	mu         sync.RWMutex
}

func NewFileSystem(fn string) (*FileSystem, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	start, end, err := readZipRegion(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	tr := &tailReader{
		r:   f,
		off: start,
	}
	zr, err := zip.NewReader(tr, end-start)
	if err != nil {
		f.Close()
		return nil, err
	}
	files := make(map[string]*zip.File)
	for _, zf := range zr.File {
		files[zf.Name] = zf
	}
	return &FileSystem{
		BinarySize: start,
		f:          f,
		zr:         zr,
		files:      files,
	}, nil
}

func readZipRegion(f *os.File) (start, end int64, err error) {
	n := int64(8 + len(suffix))
	if end, err = f.Seek(-n, 2); err != nil {
		return 0, 0, err
	}
	b := make([]byte, 8+len(suffix))
	if _, err = io.ReadFull(f, b); err != nil {
		return 0, 0, err
	}
	if !bytes.HasSuffix(b, []byte(suffix)) {
		return 0, 0, fmt.Errorf("file %s not end with %s", f.Name(), suffix)
	}
	start = int64(binary.BigEndian.Uint64(b[:8]))
	return start, end, nil
}

type tailReader struct {
	r   io.ReaderAt
	off int64
}

func (tr *tailReader) ReadAt(p []byte, off int64) (int, error) {
	return tr.r.ReadAt(p, tr.off+off)
}

func (fs *FileSystem) Open(name string) (*File, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	zf, ok := fs.files[name]
	if !ok {
		return nil, fmt.Errorf("file %s not found", name)
	}
	rc, err := zf.Open()
	if err != nil {
		return nil, err
	}
	return &File{
		FileInfo: FileInfo{
			Name: zf.Name,
			Size: int64(zf.UncompressedSize64),
		},
		ReadCloser: rc,
	}, nil
}

func (fs *FileSystem) List() []*FileInfo {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	fns := make([]*FileInfo, 0, len(fs.files))
	for _, zf := range fs.files {
		fns = append(fns, &FileInfo{
			Name: zf.Name,
			Size: int64(zf.UncompressedSize64),
		})
	}
	return fns
}

func (fs *FileSystem) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	err := fs.f.Close()
	fs.f = nil
	fs.zr = nil
	fs.files = nil
	return err
}

var (
	progFS *FileSystem
	once   sync.Once
)

func initProgFS() {
	fn := os.Args[0]
	fs, err := NewFileSystem(fn)
	if err != nil {
		panic(fmt.Sprintf("failed to init gar file system from %s, err:%v", fn, err))
	}
	progFS = fs
}

func Open(name string) (*File, error) {
	once.Do(initProgFS)
	return progFS.Open(name)
}

func List() []*FileInfo {
	once.Do(initProgFS)
	return progFS.List()
}
