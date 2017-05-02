package db

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/ghetzel/byteflood/db/metadata"
	"github.com/ghetzel/go-stockutil/pathutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot"
	"github.com/ghetzel/pivot/backends"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/filter"
	"github.com/ghetzel/pivot/mapper"
	"github.com/op/go-logging"
)

var log = logging.MustGetLogger(`byteflood/db`)

var DefaultGlobalExclusions = []string{
	`._.DS_Store`,
	`._.Trashes`,
	`.DS_Store`,
	`.Spotlight-V100`,
	`.Trashes`,
	`desktop.ini`,
	`lost+found`,
	`Thumbs.db`,
}

var DefaultBaseDirectory = `~/.config/byteflood`
var labelToPath = make(map[string]string)

type Database struct {
	BaseDirectory      string            `json:"base_dir"`
	URI                string            `json:"uri,omitempty"`
	Indexer            string            `json:"indexer,omitempty"`
	AdditionalIndexers map[string]string `json:"additional_indexers,omitempty"`
	GlobalExclusions   []string          `json:"global_exclusions,omitempty"`
	ScanInProgress     bool              `json:"scan_in_progress"`
	ExtractFields      []string          `json:"extract_fields,omitempty"`
	db                 backends.Backend
}

type Property struct {
	Key   string      `json:"key,identity"`
	Value interface{} `json:"value"`
	db    *Database
}

var Instance *Database

func NewDatabase() *Database {
	db := &Database{
		BaseDirectory:      DefaultBaseDirectory,
		GlobalExclusions:   DefaultGlobalExclusions,
		AdditionalIndexers: make(map[string]string),
	}

	return db
}

func ParseFilter(spec interface{}, fmtvalues ...interface{}) (filter.Filter, error) {
	if fmt.Sprintf("%v", spec) == `all` {
		return filter.All, nil
	}

	switch spec.(type) {
	case []string:
		return filter.Parse(strings.Join(spec.([]string), filter.CriteriaSeparator))
	case map[string]interface{}:
		return filter.FromMap(spec.(map[string]interface{}))
	case string, interface{}:
		if len(fmtvalues) > 0 {
			return filter.Parse(fmt.Sprintf(fmt.Sprintf("%v", spec), fmtvalues...))
		} else {
			return filter.Parse(fmt.Sprintf("%v", spec))
		}
	default:
		return filter.Filter{}, fmt.Errorf("Invalid argument type %T", spec)
	}
}

// Initialize the Database by opening the underlying database
func (self *Database) Initialize() error {
	filter.CriteriaSeparator = `;`
	filter.FieldTermSeparator = `=`
	filter.QueryUnescapeValues = true

	// reuse the "json:" struct tag for loading dal.Record into/out of structs
	dal.RecordStructTag = `json`

	if v, err := pathutil.ExpandUser(self.BaseDirectory); err == nil {
		self.BaseDirectory = v
	} else {
		return err
	}

	if self.URI == `` {
		self.URI = fmt.Sprintf("sqlite:///%s/info.db", self.BaseDirectory)
	}

	// if _, ok := self.AdditionalIndexers[`metadata`]; !ok {
	// 	self.AdditionalIndexers[`metadata`] = fmt.Sprintf("bleve:///%s/index", self.BaseDirectory)
	// }

	if db, err := pivot.NewDatabaseWithOptions(self.URI, backends.ConnectOptions{
		Indexer:            self.Indexer,
		AdditionalIndexers: self.AdditionalIndexers,
	}); err == nil {
		self.db = db
	} else {
		return err
	}

	if err := self.setupSchemata(); err != nil {
		return err
	}

	for _, pattern := range self.ExtractFields {
		if rx, err := regexp.Compile(pattern); err == nil {
			metadata.RegexpPatterns = append(metadata.RegexpPatterns, rx)
		} else {
			return err
		}
	}

	if err := self.refreshLabelPathCache(); err != nil {
		return err
	}

	return nil
}

// Update the label-to-realpath map (used by Entry.GetAbsolutePath)
func (self *Database) refreshLabelPathCache() error {
	var scannedDirectories []Directory

	if err := ScannedDirectories.All(&scannedDirectories); err == nil {
		for _, directory := range scannedDirectories {
			labelToPath[directory.ID] = directory.Path
		}
	} else {
		return err
	}

	return nil
}

func (self *Database) AddGlobalExclusions(patterns ...string) {
	self.GlobalExclusions = append(self.GlobalExclusions, patterns...)
}

func (self *Database) Scan(deep bool, labels ...string) error {
	if self.ScanInProgress {
		log.Warningf("Another scan is already running")
		return fmt.Errorf("Scan already running")
	} else {
		self.ScanInProgress = true

		defer func() {
			self.Cleanup()
			self.ScanInProgress = false
		}()
	}

	var scannedDirectories []Directory

	if err := ScannedDirectories.All(&scannedDirectories); err == nil {
		for _, directory := range scannedDirectories {
			// update our label-to-realpath map (used by Entry.GetAbsolutePath)
			labelToPath[directory.ID] = directory.Path

			directory.DeepScan = deep

			if len(labels) > 0 {
				skip := true

				for _, label := range labels {
					if directory.ID == stringutil.Underscore(label) {
						skip = false
						break
					}
				}

				if skip {
					log.Debugf("Skipping directory %s [%s]", directory.Path, directory.ID)
					continue
				}
			}

			if err := directory.Initialize(); err == nil {
				log.Debugf("Scanning directory %s [%s]", directory.Path, directory.ID)

				if err := directory.Scan(); err == nil {
					defer directory.RefreshStats()
					return nil
				} else {
					return err
				}
			} else {
				return err
			}
		}
	} else {
		return err
	}

	return nil
}

func (self *Database) GetDirectoriesByFile(filename string) []Directory {
	var scannedDirectories []Directory
	foundDirectories := make([]Directory, 0)

	if err := ScannedDirectories.All(&scannedDirectories); err == nil {
		for _, dir := range scannedDirectories {
			if dir.ContainsPath(filename) {
				foundDirectories = append(foundDirectories, dir)
			}
		}

		return foundDirectories
	}

	return nil
}

func (self *Database) Cleanup() error {
	if !self.ScanInProgress {
		self.ScanInProgress = true

		defer func() {
			self.ScanInProgress = false
		}()
	}

	var scannedDirectories []Directory
	var ids []string

	if err := ScannedDirectories.All(&scannedDirectories); err == nil {
		for _, dir := range scannedDirectories {
			ids = append(ids, dir.ID)
		}
	} else {
		return err
	}

	entriesToDelete := make([]interface{}, 0)

	log.Debugf("Cleaning up...")

	if f, err := ParseFilter(map[string]interface{}{
		`label`: fmt.Sprintf("not:%s", strings.Join(ids, `|`)),
	}); err == nil {
		f.Limit = -1

		backend := Metadata.GetBackend()
		indexer := backend.WithSearch(``)

		if err := indexer.DeleteQuery(MetadataSchema.Name, f); err != nil {
			log.Warningf("Remove missing labels failed: %v", err)
		}

		allQuery := filter.Copy(&filter.All)
		allQuery.Fields = []string{`id`, `name`, `label`}

		if err := Metadata.FindFunc(allQuery, Entry{}, func(entryI interface{}, err error) {
			if err == nil {
				if entry, ok := entryI.(*Entry); ok {
					if absPath, err := entry.GetAbsolutePath(); err == nil {
						if _, err := os.Stat(absPath); os.IsNotExist(err) {
							entriesToDelete = append(entriesToDelete, entry.ID)
							reportEntryDeletionStats(entry.Label, entry)
						}
					}
				}
			} else {
				log.Warningf("%v", err)
			}
		}); err == nil {
			if l := len(entriesToDelete); l > 0 {
				if err := Metadata.Delete(entriesToDelete...); err == nil {
					log.Infof("Removed %d entries", l)
				} else {
					log.Warningf("Failed to cleanup missing entries: %v", err)
				}
			}

			log.Infof("Database cleanup finished, deleted %d entries.", len(entriesToDelete))

		} else {
			log.Warningf("Failed to cleanup database: %v", err)
		}
	} else {
		return err
	}

	return nil
}

func (self *Database) setupSchemata() error {
	// register global mapper.Model instances to this database
	AuthorizedPeers = mapper.NewModel(self.db, AuthorizedPeersSchema)
	Downloads = mapper.NewModel(self.db, DownloadsSchema)
	Metadata = mapper.NewModel(self.db, MetadataSchema)
	ScannedDirectories = mapper.NewModel(self.db, ScannedDirectoriesSchema)
	Shares = mapper.NewModel(self.db, SharesSchema)
	Subscriptions = mapper.NewModel(self.db, SubscriptionsSchema)
	System = mapper.NewModel(self.db, SystemSchema)

	// set global default DB instance to us
	Instance = self

	models := []mapper.Mapper{
		AuthorizedPeers,
		Downloads,
		Metadata,
		ScannedDirectories,
		Shares,
		Subscriptions,
		System,
	}

	for _, model := range models {
		if err := model.Migrate(); err != nil {
			return err
		}
	}

	return nil
}
