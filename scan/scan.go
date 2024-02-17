package scan

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// File represents a file with its extension and an opener function
type File struct {
	Ext    string                    // Ext represents the file extension
	Opener func() (io.Reader, error) // Opener is a function to open the file and return an io.Reader
}

// Scan scans the provided paths and returns a slice of files and an error if any
func Scan(paths []string) ([]*File, error) {
	var files []*File
	err := walkPaths(paths, func(fsys fs.FS, path string) error {
		ext := strings.ToLower(filepath.Ext(path))
		opener := func() (io.Reader, error) { return fsys.Open(path) }
		if ext == ".gz" {
			ext = filepath.Ext(path[:len(path)-3])
			opener = func() (io.Reader, error) {
				if r, err := fsys.Open(path); err != nil {
					return nil, err
				} else {
					return gzip.NewReader(r)
				}
			}
		}
		files = append(files, &File{ext, opener})
		return nil
	})
	return files, err
}

// walkPaths walks through the provided paths and executes the given function on each path
func walkPaths(paths []string, fn func(fsys fs.FS, path string) error) error {
	for _, path := range paths {
		paths := []string{path}
		if strings.ContainsAny(path, "*?[") {
			var err error
			if paths, err = filepath.Glob(path); err != nil {
				if errors.Is(err, filepath.ErrBadPattern) {
					return fmt.Errorf("input path pattern %q malformed", path)
				}
				return err
			}
		}

		for _, path := range paths {
			dir, name := filepath.Split(path)
			if dir == "" {
				dir = "."
			}
			fsys := os.DirFS(dir)
			if fi, err := os.Stat(path); err != nil {
				var perr *fs.PathError
				if errors.As(err, &perr) {
					return fmt.Errorf("input path %q not found", path)
				}
				return err
			} else if fi.IsDir() {
				if err := walkDir(fsys, name, fn); err != nil {
					return err
				}
			} else if err := walkFile(fsys, name, fn); err != nil {
				return err
			}
		}
	}

	return nil
}

// walkDir walks through a directory and executes the given function on each file
func walkDir(fsys fs.FS, path string, fn func(fsys fs.FS, path string) error) error {
	return fs.WalkDir(fsys, path, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		} else {
			return walkFile(fsys, path, fn)
		}
	})
}

// walkFile walks through a file and executes the given function if it's not a zip file
func walkFile(fsys fs.FS, path string, fn func(fsys fs.FS, path string) error) error {
	if strings.EqualFold(filepath.Ext(path), ".zip") {
		if f, err := fsys.Open(path); err != nil {
			return err
		} else if s, err := f.Stat(); err != nil {
			return err
		} else {
			r, ok := f.(io.ReaderAt)
			if !ok {
				if b, err := io.ReadAll(f); err != nil {
					return err
				} else {
					r = bytes.NewReader(b)
				}
			}
			if fsys, err := zip.NewReader(r, s.Size()); err != nil {
				return err
			} else {
				return walkDir(fsys, ".", fn)
			}
		}
	} else {
		return fn(fsys, path)
	}
}
