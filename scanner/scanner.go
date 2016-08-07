package scanner

import (
	"fmt"
	"github.com/op/go-logging"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	"strings"
)

var log = logging.MustGetLogger(`scanner`)

type File struct {
	Name string
}

func (self *File) GetAlternatePath(extension string) (string, error) {
	altPath := path.Join(path.Dir(self.Name), fmt.Sprintf("%s%s",
		strings.TrimSuffix(path.Base(self.Name), path.Ext(self.Name)), extension))

	if _, err := os.Stat(altPath); err == nil {
		return altPath, nil
	} else {
		return ``, err
	}
}

func (self *File) OpenAlternate(extension string) (*os.File, error) {
	if altPath, err := self.GetAlternatePath(extension); err == nil {
		return os.Open(altPath)
	} else {
		return nil, err
	}
}

func NewFile(name string) *File {
	return &File{
		Name: name,
	}
}

type Directory struct {
	Name        string
	FilePattern string
	Files       []*File
	Directories []*Directory
}

func NewDirectory(name string, pattern string) *Directory {
	return &Directory{
		Name:        name,
		FilePattern: pattern,
		Files:       make([]*File, 0),
		Directories: make([]*Directory, 0),
	}
}

func (self *Directory) Scan() error {
	if entries, err := ioutil.ReadDir(self.Name); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Name, entry.Name())

			if entry.IsDir() {
				subdirectory := NewDirectory(absPath, self.FilePattern)

				if err := subdirectory.Scan(); err != nil {
					return err
				} else {
					log.Debugf("ADD: directory %s", subdirectory.Name)
					self.Directories = append(self.Directories, subdirectory)
				}
			} else {
				if self.FilePattern != `` {
					if rx, err := regexp.Compile(self.FilePattern); err == nil {
						if !rx.MatchString(absPath) {
							continue
						}
					} else {
						return err
					}
				}

				file := NewFile(absPath)
				log.Debugf("ADD:      file %s", file.Name)

				if _, err := file.GetAlternatePath(`.nfo`); err == nil {
					log.Debugf("ADD:        + NFO: Media Metadata")
				}

				if _, err := file.GetAlternatePath(`-thumb.jpg`); err == nil {
					log.Debugf("ADD:        + JPG: Preview Image")
				}

				if _, err := file.GetAlternatePath(`.torrent`); err == nil {
					log.Debugf("ADD:        + torrent: BitTorrent InfoHash")
				}

				self.Files = append(self.Files, file)
			}
		}
	} else {
		return err
	}

	return nil
}

type Scanner struct {
	RootDirectory *Directory
}

func NewScanner(rootDirectory string, filePattern string) *Scanner {
	return &Scanner{
		RootDirectory: NewDirectory(rootDirectory, filePattern),
	}
}

func (self *Scanner) Scan() error {
	return self.RootDirectory.Scan()
}
