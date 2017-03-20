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

var Metadata mapper.Mapper
var Shares mapper.Mapper
var Downloads mapper.Mapper
var AuthorizedPeers mapper.Mapper
var System mapper.Mapper
var ScannedDirectories mapper.Mapper
var Subscriptions mapper.Mapper

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

	if err := ScannedDirectories.All(&scannedDirectories); err == nil {
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

			if err := directory.Initialize(self); err == nil {
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

	if err := ScannedDirectories.All(&scannedDirectories); err == nil {
		for _, dir := range scannedDirectories {
			ids = append(ids, dir.ID)
		}
	} else {
		return err
	}

	filesToDelete := make([]interface{}, 0)

	log.Debugf("Cleaning up...")

	if err := Metadata.Each(File{}, func(fileI interface{}) {
		if file, ok := fileI.(*File); ok {
			if !sliceutil.ContainsString(ids, file.Label) {
				filesToDelete = append(filesToDelete, file.ID)
			} else if absPath, err := file.GetAbsolutePath(); err == nil {
				if _, err := os.Stat(absPath); os.IsNotExist(err) {
					filesToDelete = append(filesToDelete, file.ID)
				}
			}
		}
	}); err == nil {
		if l := len(filesToDelete); l > 0 {
			if err := Metadata.Delete(filesToDelete...); err == nil {
				log.Infof("Removed %d file entries", l)
			} else {
				log.Warningf("Failed to cleanup missing files: %v", err)
			}
		}

		log.Infof("Database cleanup finished, processed %d files.", len(filesToDelete))

		return nil
	} else {
		return err
	}
}

func (self *Database) setupSchemata() error {
	AuthorizedPeers = mapper.NewModel(self.db, AuthorizedPeersSchema)
	Downloads = mapper.NewModel(self.db, DownloadsSchema)
	Metadata = mapper.NewModel(self.db, MetadataSchema)
	ScannedDirectories = mapper.NewModel(self.db, ScannedDirectoriesSchema)
	Shares = mapper.NewModel(self.db, SharesSchema)
	Subscriptions = mapper.NewModel(self.db, SubscriptionsSchema)
	System = mapper.NewModel(self.db, SystemSchema)

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
