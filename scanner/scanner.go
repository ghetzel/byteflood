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

var log = logging.MustGetLogger(`byteflood.scanner`)

type File struct {
	Name            string
	RelatedSuffixes []string
}

type ScannerOptions struct {
	RecurseDirectories    bool `json:"recurse,omitempty"`
	SkipTorrentGeneration bool `json:"skip_hashing,omitempty"`
	ForceTorrentRehash    bool `json:"force_hashing,omitempty"`
	MakePublicTorrent     bool `json:"public,omitempty"`
	FileMinimumSize       int  `json:"file_min_size,omitempty"`
}

func DefaultScannerOptions() *ScannerOptions {
	return &ScannerOptions{
		RecurseDirectories: true,
	}
}

func NewFile(name string, relatedSuffixes ...string) *File {
	return &File{
		Name:            name,
		RelatedSuffixes: relatedSuffixes,
	}
}

func (self *File) Base() string {
	return strings.TrimSuffix(path.Base(self.Name), path.Ext(self.Name))
}

func (self *File) Ext() string {
	return path.Ext(self.Name)
}

func (self *File) AddExtension(extension string) (string, error) {
	return self.GetAlternatePath(self.Ext() + extension)
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

func (self *File) GetRelatedFiles() []*File {
	rv := make([]*File, 0)

	for _, suffix := range self.RelatedSuffixes {
		if subfile, err := self.GetAlternatePath(suffix); err == nil {
			rv = append(rv, NewFile(subfile))
		}
	}

	return rv
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

	file := NewFile(name, self.Scanner.RelatedFileSuffixes...)

	if file.Ext() != `.torrent` {
		var related string

		if len(file.RelatedSuffixes) > 0 {
			related = fmt.Sprintf(" (+ %s)", strings.Join(file.RelatedSuffixes, ` `))
		}

		log.Debugf("ADD:      file %s%s", file.Name, related)

		if !options.SkipTorrentGeneration {
			if torrentFilePath, err := file.AddExtension(`.torrent`); err != nil || options.ForceTorrentRehash {
				relPath := self.GetTorrentPath(file.Name)
				log.Debugf("ADD:        creating torrent %s from %s", torrentFilePath, relPath)

				torrent := CreateTorrent(self.GetPieceLength())

				if err := torrent.AddFile(relPath); err != nil {
					return nil, err
				}

				for _, subfile := range file.GetRelatedFiles() {
					if err := torrent.AddFile(subfile.Name); err != nil {
						return nil, err
					}
				}

				if announces := self.GetAnnounceUrls(); len(announces) > 0 {
					torrent.Announce = announces[0]

					if len(announces) > 1 {
						torrent.AnnounceList = announces[1:]
					}

					if options.MakePublicTorrent {
						torrent.SetPrivate(false)
					} else {
						torrent.SetPrivate(true)
					}
				}

				if torrentFile, err := os.Create(torrentFilePath); err == nil {
					err := torrent.WriteTo(torrentFile)
					torrentFile.Close()

					if err == nil {
						log.Debugf("ADD:      wrote torrent containing %d files, %d bytes",
							torrent.FileCount(), torrent.Length())
						log.Debug("")
					} else {
						return nil, err
					}
				}
			}
		}
	}

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
				// if we've specified a set of related extensions, and the current filename matches one,
				// then skip processing this file
				var shouldSkip bool

				for _, suffix := range self.Scanner.RelatedFileSuffixes {
					if strings.HasSuffix(entry.Name(), suffix) {
						shouldSkip = true
						break
					}
				}

				if shouldSkip {
					continue
				}

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
	RootDirectory       *Directory
	DirectoryPrefix     string
	PieceLength         int
	AnnounceList        []string
	RelatedFileSuffixes []string
}

func NewScanner(rootDirectory string, filePattern string) *Scanner {
	scanner := &Scanner{
		PieceLength:         DEFAULT_BF_HASH_PIECELENGTH,
		RelatedFileSuffixes: make([]string, 0),
	}

	scanner.RootDirectory = NewDirectory(scanner, rootDirectory, filePattern)

	return scanner
}

func (self *Scanner) ScanFile(name string, options *ScannerOptions) (*File, error) {
	return self.RootDirectory.ScanFile(name, options)
}

func (self *Scanner) Scan(options *ScannerOptions) error {
	return self.RootDirectory.Scan(options)
}
