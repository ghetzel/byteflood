package db

import (
	"fmt"
	"github.com/ghetzel/byteflood/db/metadata"
	"github.com/ghetzel/go-stockutil/sliceutil"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot"
	"github.com/ghetzel/pivot/backends"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/filter"
	"github.com/ghetzel/pivot/mapper"
	"github.com/op/go-logging"
	"os"
	"regexp"
	"strings"
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
var AdminUserName = `admin`

type Model interface {
	SetDatabase(*Database)
}

type Database struct {
	BaseDirectory      string            `json:"base_dir"`
	URI                string            `json:"uri,omitempty"`
	Indexer            string            `json:"indexer,omitempty"`
	AdditionalIndexers map[string]string `json:"additional_indexers,omitempty"`
	GlobalExclusions   []string          `json:"global_exclusions,omitempty"`
	ScanInProgress     bool              `json:"scan_in_progress"`
	ExtractFields      []string          `json:"extract_fields,omitempty"`
	Metadata           mapper.Mapper     `json:"-"`
	Shares             mapper.Mapper     `json:"-"`
	Downloads          mapper.Mapper     `json:"-"`
	AuthorizedPeers    mapper.Mapper     `json:"-"`
	System             mapper.Mapper     `json:"-"`
	ScannedDirectories mapper.Mapper     `json:"-"`
	Subscriptions      mapper.Mapper     `json:"-"`
	db                 backends.Backend
}

type Property struct {
	Key   string      `json:"key,identity"`
	Value interface{} `json:"value"`
	db    *Database
}

func (self *Property) SetDatabase(conn *Database) {
	self.db = conn
}

func NewDatabase() *Database {
	db := &Database{
		BaseDirectory:      DefaultBaseDirectory,
		GlobalExclusions:   DefaultGlobalExclusions,
		AdditionalIndexers: make(map[string]string),
	}

	return db
}

func ParseFilter(spec interface{}, fmtvalues ...interface{}) (filter.Filter, error) {
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

	if self.URI == `` {
		self.URI = fmt.Sprintf("sqlite:///%s/info.db", self.BaseDirectory)
	}

	if _, ok := self.AdditionalIndexers[`metadata`]; !ok {
		self.AdditionalIndexers[`metadata`] = fmt.Sprintf("bleve:///%s/index", self.BaseDirectory)
	}

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
			self.ScanInProgress = false
		}()
	}

	var scannedDirectories []Directory

	if err := self.ScannedDirectories.All(&scannedDirectories); err == nil {
		for _, directory := range scannedDirectories {
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

				if err := directory.Scan(); err != nil {
					return err
				}
			} else {
				return err
			}
		}

		if err := self.Cleanup(); err != nil {
			return err
		}
	} else {
		return err
	}

	return nil
}

func (self *Database) Cleanup() error {
	var scannedDirectories []Directory
	var ids []string

	if err := self.ScannedDirectories.All(&scannedDirectories); err == nil {
		for _, dir := range scannedDirectories {
			ids = append(ids, dir.ID)
		}
	} else {
		return err
	}

	entriesToDelete := make([]interface{}, 0)

	log.Debugf("Cleaning up...")

	if err := self.Metadata.Each(Entry{}, func(entryI interface{}) {
		if entry, ok := entryI.(*Entry); ok {
			entry.db = self

			if !sliceutil.ContainsString(ids, entry.Label) {
				entriesToDelete = append(entriesToDelete, entry.ID)
			} else if absPath, err := entry.GetAbsolutePath(); err == nil {
				if _, err := os.Stat(absPath); os.IsNotExist(err) {
					entriesToDelete = append(entriesToDelete, entry.ID)
				}
			}
		}
	}); err == nil {
		if l := len(entriesToDelete); l > 0 {
			if err := self.Metadata.Delete(entriesToDelete...); err == nil {
				log.Infof("Removed %d entries", l)
			} else {
				log.Warningf("Failed to cleanup missing entries: %v", err)
			}
		}

		log.Infof("Database cleanup finished, processed %d entries.", len(entriesToDelete))

		return nil
	} else {
		return err
	}
}

func (self *Database) setupSchemata() error {
	self.AuthorizedPeers = mapper.NewModel(self.db, AuthorizedPeersSchema)
	self.Downloads = mapper.NewModel(self.db, DownloadsSchema)
	self.Metadata = mapper.NewModel(self.db, MetadataSchema)
	self.ScannedDirectories = mapper.NewModel(self.db, ScannedDirectoriesSchema)
	self.Shares = mapper.NewModel(self.db, SharesSchema)
	self.Subscriptions = mapper.NewModel(self.db, SubscriptionsSchema)
	self.System = mapper.NewModel(self.db, SystemSchema)

	models := []mapper.Mapper{
		self.AuthorizedPeers,
		self.Downloads,
		self.Metadata,
		self.ScannedDirectories,
		self.Shares,
		self.Subscriptions,
		self.System,
	}

	for _, model := range models {
		if err := model.Migrate(); err != nil {
			return err
		}
	}

	return nil
}

func (self *Database) Initializer(instance interface{}) interface{} {
	// we want this to panic if the type cast fails
	model := instance.(Model)
	model.SetDatabase(self)
	return instance
}
