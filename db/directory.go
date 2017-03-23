package db

import (
	"fmt"
	"github.com/alexcesaro/statsd"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"io/ioutil"
	"os"
	"path"
	"regexp"
)

var stats, _ = statsd.New()

type Directory struct {
	ID                   string       `json:"id"`
	Path                 string       `json:"path"`
	Parent               string       `json:"parent"`
	RootPath             string       `json:"-"`
	FilePattern          string       `json:"file_pattern,omitempty"`
	NoRecurseDirectories bool         `json:"no_recurse,omitempty"`
	FileMinimumSize      int          `json:"min_file_size,omitempty"`
	DeepScan             bool         `json:"deep_scan,omitempty"`
	Checksum             bool         `json:"checksum"`
	Directories          []*Directory `json:"-"`
	FileCount            int          `json:"file_count"`
	db                   *Database
}

var RootDirectoryName = `root`

func NewDirectory(db *Database, dirpath string) *Directory {
	return &Directory{
		ID:          path.Base(dirpath),
		Path:        dirpath,
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
					dirEntry := NewFile(self.ID, self.RootPath, absPath)

					subdirectory := NewDirectory(self.db, absPath)

					subdirectory.ID = self.ID
					subdirectory.Parent = dirEntry.ID
					subdirectory.RootPath = self.RootPath
					subdirectory.FilePattern = self.FilePattern
					subdirectory.NoRecurseDirectories = self.NoRecurseDirectories
					subdirectory.FileMinimumSize = self.FileMinimumSize
					subdirectory.DeepScan = self.DeepScan

					log.Infof("[%s] %s: Scanning subdirectory %s", self.ID, subdirectory.Parent, subdirectory.Path)

					if err := subdirectory.Scan(); err == nil {
						self.FileCount = subdirectory.FileCount
						self.Directories = append(self.Directories, subdirectory)
					} else {
						return err
					}

					if self.FileCount == 0 {
						// cleanup files for whom we are the parent
						if f, err := ParseFilter(map[string]interface{}{
							`parent`: subdirectory.Parent,
						}); err == nil {
							if values, err := Metadata.ListWithFilter([]string{`id`}, f); err == nil {
								if ids, ok := values[`id`]; ok {
									Metadata.Delete(ids...)
								}
							} else {
								log.Errorf("[%s] Failed to cleanup files under %s: %v", self.ID, subdirectory.Parent, err)
							}
						} else {
							log.Errorf("[%s] Failed to cleanup files under %s: %v", self.ID, subdirectory.Parent, err)
						}

						if Metadata.Exists(dirEntry.ID) {
							Metadata.Delete(dirEntry.ID)
						}
					} else {
						if _, err := self.indexFile(absPath, true); err == nil {
							// cleanup files for whom we are the parent
							if err := self.cleanupMissingFiles(map[string]interface{}{
								`parent`: subdirectory.Parent,
							}); err != nil {
								log.Errorf("[%s] Failed to cleanup files under %s: %v", self.ID, subdirectory.Parent, err)
							}
						} else {
							return err
						}
					}
				}
			} else {
				// if we've specified a minimum file size, and this file is less than that,
				// then skip it
				if self.FileMinimumSize > 0 && entry.Size() < int64(self.FileMinimumSize) {
					continue
				}

				// file pattern matching
				if self.FilePattern != `` {
					if rx, err := regexp.Compile(self.FilePattern); err == nil {
						if !rx.MatchString(absPath) {
							continue
						}
					} else {
						return err
					}
				}

				// scan the file as a sharable asset
				if _, err := self.indexFile(absPath, false); err == nil {
					self.FileCount += 1
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

func (self *Directory) indexFile(name string, isDir bool) (*File, error) {
	defer stats.NewTiming().Send(`byteflood.db.entry_scan_time`)
	stats.Increment(`byteflood.db.entry`)

	if isDir {
		stats.Increment(`byteflood.db.directory`)
	} else {
		stats.Increment(`byteflood.db.file`)
	}

	// get file implementation
	file := NewFile(self.ID, self.RootPath, name)

	// skip the file if it's in the global exclusions list (case sensitive exact match)
	if sliceutil.ContainsString(self.db.GlobalExclusions, path.Base(name)) {
		return file, nil
	}

	if stat, err := os.Stat(name); err == nil {
		file.LastModifiedAt = stat.ModTime().UnixNano()

		// Deep scan: only proceed with loading metadata and updating the record if
		//   - The file is new, or...
		//   - The file exists but has been modified since we last saw it
		//
		if !self.DeepScan {
			var existingFile File

			if err := Metadata.Get(file.ID, &existingFile); err == nil {
				if file.LastModifiedAt == existingFile.LastModifiedAt {
					return &existingFile, nil
				}
			}
		}
	}

	// Deep Scan only from here on...
	// --------------------------------------------------------------------------------------------
	log.Infof("[%s] %s: Scanning file %s", self.ID, self.Parent, name)

	file.Parent = self.Parent
	file.Label = self.ID
	file.IsDirectory = isDir

	if isDir {
		file.ChildCount = self.FileCount
	}

	tm := stats.NewTiming()

	// load file metadata
	if err := file.LoadMetadata(); err != nil {
		return nil, err
	}

	// calculate checksum for file
	if self.Checksum && !file.IsDirectory {
		if sum, err := file.GenerateChecksum(); err == nil {
			file.Checksum = sum
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

	return file, nil
}

func (self *Directory) cleanupMissingFiles(query interface{}) error {
	var files []File

	if f, err := ParseFilter(query); err == nil {
		if err := Metadata.Find(f, &files); err == nil {
			filesToDelete := make([]interface{}, 0)

			for _, file := range files {
				if absPath, err := file.GetAbsolutePath(); err == nil {
					if _, err := os.Stat(absPath); os.IsNotExist(err) {
						filesToDelete = append(filesToDelete, file.ID)
					}
				} else {
					log.Warningf("[%s] Failed to cleanup missing file %s (%s)", self.ID, file.ID, file.RelativePath)
				}
			}

			if l := len(filesToDelete); l > 0 {
				if err := Metadata.Delete(filesToDelete...); err == nil {
					log.Infof("[%s] Cleaned up %d missing files", self.ID, l)
				} else {
					log.Warningf("[%s] Failed to cleanup missing files: %v", self.ID, err)
				}
			}

			return nil
		} else {
			return err
		}
	} else {
		return err
	}
}
