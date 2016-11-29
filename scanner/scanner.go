package scanner

import (
	"fmt"
	"github.com/ghetzel/byteflood/scanner/metadata"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/op/go-logging"
	"io/ioutil"
	"path"
	"regexp"
	"strings"
)

var log = logging.MustGetLogger(`byteflood/scanner`)

type File struct {
	Name     string
	Metadata map[string]interface{}
}

type ScannerOptions struct {
	RecurseDirectories bool `json:"recurse,omitempty"`
	FileMinimumSize    int  `json:"file_min_size,omitempty"`
}

func DefaultScannerOptions() *ScannerOptions {
	return &ScannerOptions{
		RecurseDirectories: true,
	}
}

func NewFile(name string) *File {
	return &File{
		Name:     name,
		Metadata: make(map[string]interface{}),
	}
}

func (self *File) LoadMetadata() error {
	for _, loader := range metadata.GetLoadersForFile(self.Name) {
		if data, err := loader.LoadMetadata(self.Name); err == nil {
			self.Metadata[self.normalizeLoaderName(loader)] = data
		} else {
			return err
		}
	}

	return nil
}

func (self *File) normalizeLoaderName(loader metadata.Loader) string {
	name := stringutil.Underscore(strings.Replace(fmt.Sprintf("%T", loader), `Loader`, ``, -1))
	return name
}

type Directory struct {
	Scanner     *Scanner
	Name        string
	FilePattern string
	Files       []*File
	Directories []*Directory
}

func NewDirectory(scanner *Scanner, name string, pattern string) *Directory {
	return &Directory{
		Scanner:     scanner,
		Name:        name,
		FilePattern: pattern,
		Files:       make([]*File, 0),
		Directories: make([]*Directory, 0),
	}
}

func (self *Directory) ScanFile(name string, options *ScannerOptions) (*File, error) {
	// file pattern matching
	if self.FilePattern != `` {
		if rx, err := regexp.Compile(self.FilePattern); err == nil {
			if !rx.MatchString(name) {
				return nil, nil
			}
		} else {
			return nil, err
		}
	}

	// get file implementation
	file := NewFile(name)

	// load file metadata
	if err := file.LoadMetadata(); err != nil {
		return nil, err
	}

	log.Debugf("Got file %+v", file)

	return file, nil
}

func (self *Directory) Scan(options *ScannerOptions) error {
	if entries, err := ioutil.ReadDir(self.Name); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Name, entry.Name())

			// recursive directory handling
			if entry.IsDir() {
				if options.RecurseDirectories {
					subdirectory := NewDirectory(self.Scanner, absPath, self.FilePattern)

					if err := subdirectory.Scan(options); err != nil {
						return err
					} else {
						log.Debugf("ADD: directory %s", subdirectory.Name)
						self.Directories = append(self.Directories, subdirectory)
					}
				}
			} else {
				// if we've specified a minimum file size, and this file is less than that,
				// then skip it
				if options.FileMinimumSize > 0 && entry.Size() < int64(options.FileMinimumSize) {
					continue
				}

				// scan the file as a sharable asset
				if file, err := self.ScanFile(absPath, options); err == nil {
					if file != nil {
						self.Files = append(self.Files, file)
					}
				} else {
					return err
				}
			}
		}
	} else {
		return err
	}

	return nil
}

type Scanner struct {
	RootDirectory   *Directory
	DirectoryPrefix string
}

func NewScanner(rootDirectory string, filePattern string) *Scanner {
	scanner := &Scanner{}

	scanner.RootDirectory = NewDirectory(scanner, rootDirectory, filePattern)

	return scanner
}

func (self *Scanner) ScanFile(name string, options *ScannerOptions) (*File, error) {
	return self.RootDirectory.ScanFile(name, options)
}

func (self *Scanner) Scan(options *ScannerOptions) error {
	return self.RootDirectory.Scan(options)
}
