package scanner

import (
	"fmt"
	"github.com/ghetzel/go-stockutil/sliceutil"
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

func (self *File) Base() string {
	return strings.TrimSuffix(path.Base(self.Name), path.Ext(self.Name))
}

func (self *File) GetAlternatePath(extension string) (string, error) {
	altPath := path.Join(path.Dir(self.Name), fmt.Sprintf("%s%s",
		self.Base(), extension))

	if _, err := os.Stat(altPath); err == nil {
		return altPath, nil
	} else {
		return altPath, err
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

func (self *Directory) GetPieceLength() int {
	if self.Scanner == nil || self.Scanner.PieceLength == 0 {
		return DEFAULT_BF_HASH_PIECELENGTH
	} else {
		return self.Scanner.PieceLength
	}
}

func (self *Directory) GetAnnounceUrls() []string {
	if self.Scanner != nil && len(self.Scanner.AnnounceList) > 0 {
		return self.Scanner.AnnounceList
	} else {
		return []string{}
	}
}

func (self *Directory) GetTorrentPath(in string) string {
	return strings.TrimPrefix(in, `./`)
}

func (self *Directory) ScanFile(name string, tags ...string) (*File, error) {
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

	file := NewFile(name)
	log.Debugf("ADD:      file %s", file.Name)

	if !sliceutil.ContainsString(tags, `no-torrent`) {
		if torrentFilePath, err := file.GetAlternatePath(`.torrent`); err != nil || sliceutil.ContainsString(tags, `rehash`) {
			relPath := self.GetTorrentPath(file.Name)
			log.Debugf("ADD:        creating torrent %s from %s", torrentFilePath, relPath)

			if torrent, err := CreateTorrent(relPath, self.GetPieceLength()); err == nil {
				if announces := self.GetAnnounceUrls(); len(announces) > 0 {
					torrent.Announce = announces[0]

					if len(announces) > 1 {
						torrent.AnnounceList = announces[1:]
					}

					if sliceutil.ContainsString(tags, `public`) {
						torrent.SetPrivate(false)
					} else {
						torrent.SetPrivate(true)
					}
				}

				if torrentFile, err := os.Create(torrentFilePath); err == nil {
					err := torrent.WriteTo(torrentFile)
					torrentFile.Close()

					if err == nil {
						log.Debugf("ADD:        wrote torrent %s", torrentFilePath)
					} else {
						return nil, err
					}
				}
			} else {
				return nil, err
			}
		}
	}

	return file, nil
}

func (self *Directory) Scan(tags ...string) error {
	if entries, err := ioutil.ReadDir(self.Name); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Name, entry.Name())

			// recursive directory handling
			if entry.IsDir() {
				if !sliceutil.ContainsString(tags, `no-recurse`) {
					subdirectory := NewDirectory(self.Scanner, absPath, self.FilePattern)

					if err := subdirectory.Scan(tags...); err != nil {
						return err
					} else {
						log.Debugf("ADD: directory %s", subdirectory.Name)
						self.Directories = append(self.Directories, subdirectory)
					}
				}
			} else {
				if file, err := self.ScanFile(absPath, tags...); err == nil {
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
	PieceLength     int
	AnnounceList    []string
}

func NewScanner(rootDirectory string, filePattern string) *Scanner {
	scanner := &Scanner{
		PieceLength: DEFAULT_BF_HASH_PIECELENGTH,
	}

	scanner.RootDirectory = NewDirectory(scanner, rootDirectory, filePattern)

	return scanner
}

func (self *Scanner) ScanFile(name string, tags ...string) (*File, error) {
	return self.RootDirectory.ScanFile(name, tags...)
}

func (self *Scanner) Scan(tags ...string) error {
	return self.RootDirectory.Scan(tags...)
}
