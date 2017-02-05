package db

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/alexcesaro/statsd"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"io"
	"io/ioutil"
	"os"
	"path"
	"regexp"
	// "time"
)

var stats, _ = statsd.New()

type Directory struct {
	ID                   int          `json:"id"`
	Path                 string       `json:"path"`
	Parent               string       `json:"parent"`
	Label                string       `json:"label,omitempty"`
	RootPath             string       `json:"-"`
	FilePattern          string       `json:"file_patterns,omitempty"`
	NoRecurseDirectories bool         `json:"no_recurse,omitempty"`
	FileMinimumSize      int          `json:"min_file_size,omitempty"`
	QuickScan            bool         `json:"quick_scan,omitempty"`
	Checksum             bool         `json:"checksum"`
	Directories          []*Directory `json:"-"`
	db                   *Database
}

var RootDirectoryName = `root`

func NewDirectory(db *Database, path string) *Directory {
	return &Directory{
		Path:        path,
		Parent:      RootDirectoryName,
		Directories: make([]*Directory, 0),
		Checksum:    true,
		db:          db,
	}
}

func (self *Directory) Initialize(db *Database) error {
	if self.Path == `` {
		return fmt.Errorf("Directory path must be specified.")
	} else {
		if p, err := pathutil.ExpandUser(self.Path); err == nil {
			self.Path = p
		} else {
			return err
		}
	}

	if self.Label == `` {
		self.Label = stringutil.Underscore(path.Base(self.Path))
	}

	if self.RootPath == `` {
		self.RootPath = self.Path
	}

	if self.Parent == `` {
		self.Parent = RootDirectoryName
	}

	self.db = db

	return nil
}

func (self *Directory) Scan() error {
	if entries, err := ioutil.ReadDir(self.Path); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Path, entry.Name())

			// recursive directory handling
			if entry.IsDir() {
				if !self.NoRecurseDirectories {
					if dirEntry, err := self.indexFile(absPath, true); err == nil {
						subdirectory := NewDirectory(self.db, absPath)

						subdirectory.Parent = dirEntry.ID
						subdirectory.Label = self.Label
						subdirectory.RootPath = self.RootPath
						subdirectory.FilePattern = self.FilePattern
						subdirectory.NoRecurseDirectories = self.NoRecurseDirectories
						subdirectory.FileMinimumSize = self.FileMinimumSize
						subdirectory.QuickScan = self.QuickScan

						log.Debugf("[%s] %s: Scanning subdirectory %s", self.Label, subdirectory.Parent, subdirectory.Path)

						if err := subdirectory.Scan(); err == nil {
							self.Directories = append(self.Directories, subdirectory)
						} else {
							return err
						}
					} else {
						return err
					}
				}
			} else {
				// if we've specified a minimum file size, and this file is less than that,
				// then skip it
				if self.FileMinimumSize > 0 && entry.Size() < int64(self.FileMinimumSize) {
					continue
				}

				// scan the file as a sharable asset
				if _, err := self.indexFile(absPath, false); err != nil {
					return err
				}
			}
		}
	} else {
		return err
	}

	return nil
}

func (self *Directory) indexFile(name string, isDir bool) (*File, error) {
	defer stats.NewTiming().Send(`byteflood.db.entry_scan_time`)
	stats.Increment(`byteflood.db.entry`)

	if isDir {
		stats.Increment(`byteflood.db.directory`)
	} else {
		stats.Increment(`byteflood.db.file`)
	}

	// get file implementation
	file := NewFile(self.Label, self.RootPath, name)

	// skip the file if it's in the global exclusions list (case sensitive exact match)
	if sliceutil.ContainsString(self.db.GlobalExclusions, path.Base(name)) {
		return file, nil
	}

	if !isDir {
		// file pattern matching
		if self.FilePattern != `` {
			if rx, err := regexp.Compile(self.FilePattern); err == nil {
				if !rx.MatchString(name) {
					return file, nil
				}
			} else {
				return nil, err
			}
		}
	}

	// unless we're forcing the scan, see if we can skip this file
	// if !self.db.ForceRescan {
	// 	if stat, err := os.Stat(name); err == nil {
	// 		if record, err := self.db.RetrieveRecord(file.ID); err == nil {
	// 			lastModifiedAt := record.Get(`last_modified_at`, int64(0))

	// 			if epochNs, ok := lastModifiedAt.(int64); ok {
	// 				if !stat.ModTime().After(time.Unix(0, epochNs)) {
	// 					return file, nil
	// 				}
	// 			}
	// 		}

	// 		file.LastModifiedAt = stat.ModTime().UnixNano()
	// 	} else {
	// 		return nil, err
	// 	}
	// }

	file.Parent = self.Parent
	file.Label = self.Label
	file.IsDirectory = isDir

	tm := stats.NewTiming()

	// load file metadata
	if err := file.LoadMetadata(); err != nil {
		return nil, err
	}

	// calculate checksum for file
	if self.Checksum && !file.IsDirectory {
		if fsFile, err := os.Open(name); err == nil {
			hash := sha256.New()

			if _, err := io.Copy(hash, fsFile); err != nil {
				return nil, err
			}

			result := hash.Sum(nil)
			file.Checksum = hex.EncodeToString([]byte(result[:]))
		} else {
			return nil, err
		}
	}

	tm.Send(`byteflood.db.entry_metadata_load_time`)
	tm = stats.NewTiming()

	// persist the file record
	if err := Metadata.CreateOrUpdate(file.ID, file); err != nil {
		return nil, err
	}

	tm.Send(`byteflood.db.entry_persist_time`)
	tm = stats.NewTiming()

	// store the absolute filesystem path separately
	self.db.PropertySet(fmt.Sprintf("metadata.paths.%s", file.ID), name)

	tm.Send(`byteflood.db.entry_sysprop_time`)

	return file, nil
}
