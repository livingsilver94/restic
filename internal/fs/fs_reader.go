package fs

import (
	"fmt"
	"io"
	"os"
	"path"
	"sync"
	"syscall"
	"time"

	"github.com/restic/restic/internal/errors"
	"github.com/restic/restic/internal/restic"
)

// Reader is a file system which provides a directory with a single file. When
// this file is opened for reading, the reader is passed through. The file can
// be opened once, all subsequent open calls return syscall.EIO. For Lstat(),
// the provided FileInfo is returned.
type Reader struct {
	Name string
	io.ReadCloser

	// for FileInfo
	Mode    os.FileMode
	ModTime time.Time
	Size    int64

	AllowEmptyFile bool

	open sync.Once
}

// statically ensure that Local implements FS.
var _ FS = &Reader{}

// VolumeName returns leading volume name, for the Reader file system it's
// always the empty string.
func (fs *Reader) VolumeName(_ string) string {
	return ""
}

func (fs *Reader) fi() os.FileInfo {
	return fakeFileInfo{
		name:    fs.Name,
		size:    fs.Size,
		mode:    fs.Mode,
		modtime: fs.ModTime,
	}
}

// OpenFile is the generalized open call; most users will use Open
// or Create instead.  It opens the named file with specified flag
// (O_RDONLY etc.) and perm, (0666 etc.) if applicable.  If successful,
// methods on the returned File can be used for I/O.
// If there is an error, it will be of type *os.PathError.
func (fs *Reader) OpenFile(name string, flag int, _ os.FileMode) (f File, err error) {
	if flag & ^(O_RDONLY|O_NOFOLLOW) != 0 {
		return nil, pathError("open", name,
			fmt.Errorf("invalid combination of flags 0x%x", flag))
	}

	switch name {
	case fs.Name:
		fs.open.Do(func() {
			f = newReaderFile(fs.ReadCloser, fs.fi(), fs.AllowEmptyFile)
		})

		if f == nil {
			return nil, pathError("open", name, syscall.EIO)
		}

		return f, nil
	case "/", ".":
		f = fakeDir{
			entries: []os.FileInfo{fs.fi()},
		}
		return f, nil
	}

	return nil, pathError("open", name, syscall.ENOENT)
}

// Stat returns a FileInfo describing the named file. If there is an error, it
// will be of type *os.PathError.
func (fs *Reader) Stat(name string) (os.FileInfo, error) {
	return fs.Lstat(name)
}

// Lstat returns the FileInfo structure describing the named file.
// If the file is a symbolic link, the returned FileInfo
// describes the symbolic link.  Lstat makes no attempt to follow the link.
// If there is an error, it will be of type *os.PathError.
func (fs *Reader) Lstat(name string) (os.FileInfo, error) {
	getDirInfo := func(name string) os.FileInfo {
		fi := fakeFileInfo{
			name:    fs.Base(name),
			size:    0,
			mode:    os.ModeDir | 0755,
			modtime: time.Now(),
		}
		return fi
	}

	switch name {
	case fs.Name:
		return fs.fi(), nil
	case "/", ".":
		return getDirInfo(name), nil
	}

	dir := fs.Dir(fs.Name)
	for {
		if dir == "/" || dir == "." {
			break
		}
		if name == dir {
			return getDirInfo(name), nil
		}
		dir = fs.Dir(dir)
	}

	return nil, pathError("lstat", name, os.ErrNotExist)
}

func (fs *Reader) DeviceID(_ os.FileInfo) (deviceID uint64, err error) {
	return 0, errors.New("Device IDs are not supported")
}

func (fs *Reader) ExtendedStat(fi os.FileInfo) ExtendedFileInfo {
	return ExtendedFileInfo{
		FileInfo: fi,
	}
}

func (fs *Reader) NodeFromFileInfo(path string, fi os.FileInfo, _ bool) (*restic.Node, error) {
	node := buildBasicNode(path, fi)

	// fill minimal info with current values for uid, gid
	node.UID = uint32(os.Getuid())
	node.GID = uint32(os.Getgid())
	node.ChangeTime = node.ModTime

	return node, nil
}

// Join joins any number of path elements into a single path, adding a
// Separator if necessary. Join calls Clean on the result; in particular, all
// empty strings are ignored. On Windows, the result is a UNC path if and only
// if the first path element is a UNC path.
func (fs *Reader) Join(elem ...string) string {
	return path.Join(elem...)
}

// Separator returns the OS and FS dependent separator for dirs/subdirs/files.
func (fs *Reader) Separator() string {
	return "/"
}

// IsAbs reports whether the path is absolute. For the Reader, this is always the case.
func (fs *Reader) IsAbs(_ string) bool {
	return true
}

// Abs returns an absolute representation of path. If the path is not absolute
// it will be joined with the current working directory to turn it into an
// absolute path. The absolute path name for a given file is not guaranteed to
// be unique. Abs calls Clean on the result.
//
// For the Reader, all paths are absolute.
func (fs *Reader) Abs(p string) (string, error) {
	return path.Clean(p), nil
}

// Clean returns the cleaned path. For details, see filepath.Clean.
func (fs *Reader) Clean(p string) string {
	return path.Clean(p)
}

// Base returns the last element of p.
func (fs *Reader) Base(p string) string {
	return path.Base(p)
}

// Dir returns p without the last element.
func (fs *Reader) Dir(p string) string {
	return path.Dir(p)
}

func newReaderFile(rd io.ReadCloser, fi os.FileInfo, allowEmptyFile bool) *readerFile {
	return &readerFile{
		ReadCloser:     rd,
		AllowEmptyFile: allowEmptyFile,
		fakeFile: fakeFile{
			FileInfo: fi,
			name:     fi.Name(),
		},
	}
}

type readerFile struct {
	io.ReadCloser
	AllowEmptyFile, bytesRead bool

	fakeFile
}

// ErrFileEmpty is returned inside a *os.PathError by Read() for the file
// opened from the fs provided by Reader when no data could be read and
// AllowEmptyFile is not set.
var ErrFileEmpty = errors.New("no data read")

func (r *readerFile) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.bytesRead = true
	}

	// return an error if we did not read any data
	if err == io.EOF && !r.AllowEmptyFile && !r.bytesRead {
		return n, pathError("read", r.fakeFile.name, ErrFileEmpty)
	}

	return n, err
}

func (r *readerFile) Close() error {
	return r.ReadCloser.Close()
}

// ensure that readerFile implements File
var _ File = &readerFile{}

// fakeFile implements all File methods, but only returns errors for anything
// except Stat()
type fakeFile struct {
	name string
	os.FileInfo
}

// ensure that fakeFile implements File
var _ File = fakeFile{}

func (f fakeFile) Readdirnames(_ int) ([]string, error) {
	return nil, pathError("readdirnames", f.name, os.ErrInvalid)
}

func (f fakeFile) Read(_ []byte) (int, error) {
	return 0, pathError("read", f.name, os.ErrInvalid)
}

func (f fakeFile) Close() error {
	return nil
}

func (f fakeFile) Stat() (os.FileInfo, error) {
	return f.FileInfo, nil
}

// fakeDir implements Readdirnames and Readdir, everything else is delegated to fakeFile.
type fakeDir struct {
	entries []os.FileInfo
	fakeFile
}

func (d fakeDir) Readdirnames(n int) ([]string, error) {
	if n > 0 {
		return nil, pathError("readdirnames", d.name, errors.New("not implemented"))
	}
	names := make([]string, 0, len(d.entries))
	for _, entry := range d.entries {
		names = append(names, entry.Name())
	}

	return names, nil
}

// fakeFileInfo implements the bare minimum of os.FileInfo.
type fakeFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modtime time.Time
}

func (fi fakeFileInfo) Name() string {
	return fi.name
}

func (fi fakeFileInfo) Size() int64 {
	return fi.size
}

func (fi fakeFileInfo) Mode() os.FileMode {
	return fi.mode
}

func (fi fakeFileInfo) ModTime() time.Time {
	return fi.modtime
}

func (fi fakeFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir > 0
}

func (fi fakeFileInfo) Sys() interface{} {
	return nil
}

func pathError(op, name string, err error) *os.PathError {
	return &os.PathError{Op: op, Path: name, Err: err}
}
