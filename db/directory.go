package db

import (
	"fmt"
	"github.com/alexcesaro/statsd"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/sabhiram/go-gitignore"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var stats, _ = statsd.New()

type Directory struct {
	ID                   string       `json:"id"`
	Path                 string       `json:"path"`
	Parent               string       `json:"parent"`
	RootPath             string       `json:"-"`
	FilePattern          string       `json:"file_pattern,omitempty"`
	NoRecurseDirectories bool         `json:"no_recurse"`
	FollowSymlinks       bool         `json:"follow_symlinks"`
	FileMinimumSize      int          `json:"min_file_size,omitempty"`
	DeepScan             bool         `json:"deep_scan,omitempty"`
	Directories          []*Directory `json:"-"`
	FileCount            int          `json:"file_count"`
	db                   *Database
	compiledIgnoreList   *ignore.GitIgnore
}

func GetScannedDirectories(conn *Database) ([]*Directory, error) {
	var dirs []*Directory

	if err := conn.ScannedDirectories.All(&dirs); err == nil {
		return dirs, nil
	} else {
		return nil, err
	}
}

var RootDirectoryName = `root`

type WalkEntryFunc func(entry *Entry) error // {}

func (self *Directory) SetDatabase(conn *Database) {
	self.db = conn
}

func (self *Directory) Constructor() interface{} {
	if self.ID == `` && self.Path != `` {
		self.ID = path.Base(self.Path)
	}

	return self
}

func (self *Directory) Initialize() error {
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

	self.RootPath = strings.TrimSuffix(self.RootPath, `/`)

	if self.Parent == `` {
		self.Parent = RootDirectoryName
	}

	// file pattern matching
	if self.FilePattern != `` {
		if ig, err := ignore.CompileIgnoreLines(strings.Split(self.FilePattern, "\n")...); err == nil {
			self.compiledIgnoreList = ig
		} else {
			return err
		}
	}

	return nil
}

func (self *Directory) Scan() error {
	if entries, err := ioutil.ReadDir(self.Path); err == nil {
		for _, entry := range entries {
			absPath := path.Join(self.Path, entry.Name())
			relPath := strings.TrimPrefix(absPath, self.RootPath)

			if pathutil.IsSymlink(entry.Mode()) {
				if self.FollowSymlinks {
					if realpath, err := os.Readlink(absPath); err == nil {
						if realAbsPath, err := filepath.Abs(path.Join(self.Path, realpath)); err == nil {
							if realstat, err := os.Stat(realAbsPath); err == nil {
								log.Infof("[%s] Following symbolic link %s -> %s", self.ID, absPath, realAbsPath)
								entry = realstat
							} else {
								log.Warningf("[%s] Error reading target of symbolic link %s: %v", self.ID, realAbsPath, err)
								continue
							}
						} else {
							log.Warningf("[%s] Error following symbolic link %s: %v", self.ID, realpath, err)
							continue
						}
					} else {
						log.Warningf("[%s] Error reading symbolic link %s: %v", self.ID, entry.Name(), err)
						continue
					}
				} else {
					log.Infof("[%s] Skipping symbolic link %s", self.ID, absPath)
					continue
				}
			}

			// if an ignore list is in effect for this directory
			if self.compiledIgnoreList != nil {
				if self.compiledIgnoreList.MatchesPath(relPath) {
					log.Debugf("[%s] Ignoring entry %s", self.ID, relPath)
					continue
				}
			}

			// recursive directory handling
			if entry.IsDir() {
				if !self.NoRecurseDirectories {
					dirEntry := NewEntry(self.db, self.ID, self.RootPath, absPath)

					if subdirectory, ok := ScannedDirectoriesSchema.NewInstance().(*Directory); ok {
						subdirectory.ID = self.ID
						subdirectory.Path = absPath
						subdirectory.Parent = dirEntry.ID
						subdirectory.RootPath = self.RootPath
						subdirectory.FilePattern = self.FilePattern
						subdirectory.NoRecurseDirectories = self.NoRecurseDirectories
						subdirectory.FileMinimumSize = self.FileMinimumSize
						subdirectory.FollowSymlinks = self.FollowSymlinks
						subdirectory.DeepScan = self.DeepScan
						subdirectory.compiledIgnoreList = self.compiledIgnoreList

						if err := subdirectory.Initialize(); err == nil {
							log.Infof("[%s] %16s: Scanning subdirectory %s", self.ID, subdirectory.Parent, relPath)

							if err := subdirectory.Scan(); err == nil {
								self.FileCount = subdirectory.FileCount
								self.Directories = append(self.Directories, subdirectory)
							} else {
								return err
							}
						} else {
							return err
						}

						if self.FileCount == 0 {
							// cleanup entries for whom we are the parent
							if f, err := ParseFilter(map[string]interface{}{
								`parent`: subdirectory.Parent,
							}); err == nil {
								if values, err := self.db.Metadata.ListWithFilter([]string{`id`}, f); err == nil {
									if ids, ok := values[`id`]; ok {
										self.db.Metadata.Delete(ids...)
									}
								} else {
									log.Errorf("[%s] Failed to cleanup entries under %s: %v", self.ID, subdirectory.Parent, err)
								}
							} else {
								log.Errorf("[%s] Failed to cleanup entries under %s: %v", self.ID, subdirectory.Parent, err)
							}

							if self.db.Metadata.Exists(dirEntry.ID) {
								self.db.Metadata.Delete(dirEntry.ID)
							}
						} else {
							if _, err := self.scanEntry(absPath, true); err == nil {
								// cleanup entries for whom we are the parent
								if err := self.cleanupMissingEntries(map[string]interface{}{
									`parent`: subdirectory.Parent,
								}); err != nil {
									log.Errorf("[%s] Failed to cleanup entries under %s: %v", self.ID, subdirectory.Parent, err)
								}
							} else {
								return err
							}
						}
					} else {
						return fmt.Errorf("Failed to instantiate new Directory")
					}
				}
			} else {
				// if we've specified a minimum file size, and this file is less than that,
				// then skip it
				if self.FileMinimumSize > 0 && entry.Size() < int64(self.FileMinimumSize) {
					continue
				}

				// scan the entry as a sharable asset
				if _, err := self.scanEntry(absPath, false); err == nil {
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

func (self *Directory) WalkModifiedSince(lastModifiedAt time.Time, entryFn WalkEntryFunc) error {
	return filepath.Walk(self.Path, func(name string, info os.FileInfo, err error) error {
		if info.ModTime().After(lastModifiedAt) {
			return entryFn(NewEntry(self.db, self.ID, self.RootPath, name))
		}

		return nil
	})
}

func (self *Directory) scanEntry(name string, isDir bool) (*Entry, error) {
	defer stats.NewTiming().Send(`byteflood.db.entry_scan_time`)
	stats.Increment(`byteflood.db.entry`)

	if isDir {
		stats.Increment(`byteflood.db.directory`)
	} else {
		stats.Increment(`byteflood.db.entry`)
	}

	// get entry implementation
	entry := NewEntry(self.db, self.ID, self.RootPath, name)

	// skip the entry if it's in the global exclusions list (case sensitive exact match)
	if sliceutil.ContainsString(self.db.GlobalExclusions, path.Base(name)) {
		return entry, nil
	}

	if stat, err := os.Stat(name); err == nil {
		entry.Size = stat.Size()
		entry.LastModifiedAt = stat.ModTime().UnixNano()

		// Deep scan: only proceed with loading metadata and updating the record if
		//   - The entry is new, or...
		//   - The entry exists but has been modified since we last saw it
		//
		if !self.DeepScan {
			var existingFile Entry

			if err := self.db.Metadata.Get(entry.ID, &existingFile); err == nil {
				if entry.LastModifiedAt == existingFile.LastModifiedAt {
					return &existingFile, nil
				}
			}
		}
	}

	// Deep Scan only from here on...
	// --------------------------------------------------------------------------------------------
	log.Infof("[%s] %16s: Scanning entry %s", self.ID, self.Parent, name)

	entry.Parent = self.Parent
	entry.Label = self.ID
	entry.IsDirectory = isDir

	if isDir {
		entry.ChildCount = self.FileCount
	}

	tm := stats.NewTiming()

	// load entry metadata
	if err := entry.LoadMetadata(); err != nil {
		return nil, err
	}

	// calculate checksum for entry
	if !entry.IsDirectory {
		if sum, err := entry.GenerateChecksum(false); err == nil {
			entry.Checksum = sum
		} else {
			return nil, err
		}
	}

	tm.Send(`byteflood.db.entry_metadata_load_time`)
	tm = stats.NewTiming()

	// persist the entry record
	if err := self.db.Metadata.CreateOrUpdate(entry.ID, entry); err != nil {
		return nil, err
	}

	tm.Send(`byteflood.db.entry_persist_time`)

	return entry, nil
}

func (self *Directory) cleanupMissingEntries(query interface{}) error {
	var entries []Entry

	if f, err := ParseFilter(query); err == nil {
		if err := self.db.Metadata.Find(f, &entries); err == nil {
			entriesToDelete := make([]interface{}, 0)

			for _, entry := range entries {
				entry.SetDatabase(self.db)

				if self.compiledIgnoreList != nil {
					if self.compiledIgnoreList.MatchesPath(entry.RelativePath) {
						entriesToDelete = append(entriesToDelete, entry.ID)
						continue
					}
				}

				if absPath, err := entry.GetAbsolutePath(); err == nil {
					if _, err := os.Stat(absPath); os.IsNotExist(err) {
						entriesToDelete = append(entriesToDelete, entry.ID)
					}
				} else {
					log.Warningf("[%s] Failed to cleanup missing entry %s (%s)", self.ID, entry.ID, entry.RelativePath)
				}
			}

			if l := len(entriesToDelete); l > 0 {
				if err := self.cleanup(entriesToDelete...); err == nil {
					log.Infof("[%s] Cleaned up %d missing entries", self.ID, l)
				} else {
					log.Warningf("[%s] Failed to cleanup missing entries: %v", self.ID, err)
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

func (self *Directory) cleanup(entries ...interface{}) error {
	if err := self.db.Metadata.Delete(entries...); err == nil {
		return nil
	} else {
		return err
	}
}
