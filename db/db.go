package db

import (
	"fmt"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot"
	"github.com/ghetzel/pivot/backends"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/filter"
	"github.com/ghetzel/pivot/mapper"
	"github.com/op/go-logging"
	"os"
	"strings"
	"time"
)

var log = logging.MustGetLogger(`byteflood/scanner`)

var Metadata *mapper.Model
var Shares *mapper.Model
var Downloads *mapper.Model
var AuthorizedPeers *mapper.Model
var System *mapper.Model
var ScannedDirectories *mapper.Model
var Subscriptions *mapper.Model

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

type Database struct {
	URI              string   `json:"uri,omitempty"`
	Indexer          string   `json:"indexer,omitempty"`
	ScanInProgress   bool     `json:"scan_in_progress"`
	GlobalExclusions []string `json:"global_exclusions,omitempty"`
	ForceRescan      bool
	db               backends.Backend
}

type KV struct {
	Key   string      `json:"key,identity"`
	Value interface{} `json:"value"`
}

func NewDatabase() *Database {
	return &Database{
		URI: `sqlite:///~/.config/byteflood/info.db`,
		// Indexer:          `bleve:///~/.config/byteflood/index`,
		GlobalExclusions: DefaultGlobalExclusions,
	}
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

	// reuse the "json:" struct tag for loading dal.Record into/out of structs
	dal.RecordStructTag = `json`

	if db, err := pivot.NewDatabaseWithOptions(self.URI, backends.ConnectOptions{
		Indexer: self.Indexer,
	}); err == nil {
		self.db = db
	} else {
		return err
	}

	if err := self.setupSchemata(); err != nil {
		return err
	}

	return nil
}

func (self *Database) AddGlobalExclusions(patterns ...string) {
	self.GlobalExclusions = append(self.GlobalExclusions, patterns...)
}

// Query records in the given collection
func (self *Database) Query(collectionName string, f filter.Filter) (*dal.RecordSet, error) {
	if index := self.db.WithSearch(); index != nil {
		return index.Query(collectionName, f)
	} else {
		return nil, fmt.Errorf("Backend type %T does not support searching", self.db)
	}
}

// List distinct values from the given collection
func (self *Database) List(collectionName string, fields []string, f filter.Filter) (map[string][]interface{}, error) {
	if index := self.db.WithSearch(); index != nil {
		return index.ListValues(collectionName, fields, f)
	} else {
		return nil, fmt.Errorf("Backend type %T does not support searching", self.db)
	}
}

// Return whethere a metadata record with the given ID exists
func (self *Database) RecordExists(id string) bool {
	return self.db.Exists(MetadataSchema.Name, id)
}

// Retrieve a metadata record by ID
func (self *Database) RetrieveRecord(id string) (*dal.Record, error) {
	if record, err := self.db.Retrieve(MetadataSchema.Name, id); err == nil {
		return record, nil
	} else {
		return nil, err
	}
}

// Save a given metadata record
func (self *Database) PersistRecord(id string, data map[string]interface{}) error {
	if self.RecordExists(id) {
		return self.db.Update(MetadataSchema.Name, dal.NewRecordSet(
			dal.NewRecord(id).SetFields(data),
		))
	} else {
		return self.db.Insert(MetadataSchema.Name, dal.NewRecordSet(
			dal.NewRecord(id).SetFields(data),
		))
	}
}

// Delete metadata records that match the given set of IDs
func (self *Database) DeleteRecords(ids ...string) error {
	return self.db.Delete(MetadataSchema.Name, ids)
}

// Query records from the metadata collection
func (self *Database) QueryMetadata(filterString string) (*dal.RecordSet, error) {
	if f, err := ParseFilter(filterString); err == nil {
		return self.Query(MetadataSchema.Name, f)
	} else {
		return nil, err
	}
}

// Lists distinct values of the given field from the metadata collection.
func (self *Database) ListMetadata(fields []string, f ...filter.Filter) (map[string][]interface{}, error) {
	var ft filter.Filter

	if len(f) > 0 {
		ft = f[0]
	} else {
		ft = filter.All
	}

	return self.List(MetadataSchema.Name, fields, ft)
}

func (self *Database) PropertySet(key string, value interface{}, fields ...map[string]interface{}) error {
	record := dal.NewRecord(key)

	if len(fields) > 0 {
		record.SetFields(fields[0])
	}

	record.Set(`key`, key)
	record.Set(`value`, value)

	return System.Create(record)
}

func (self *Database) PropertyGet(key string, fallback ...interface{}) interface{} {
	var kv KV

	if err := System.Get(key, &kv); err == nil && kv.Value != nil {
		return kv.Value
	} else {
		if len(fallback) > 0 {
			return fallback[0]
		} else {
			return nil
		}
	}
}

func (self *Database) GetFileAbsolutePath(id string) (string, error) {
	if Metadata.Exists(id) {
		if v := self.PropertyGet(fmt.Sprintf("metadata.paths.%s", id)); v != nil {
			absPath := fmt.Sprintf("%v", v)

			if _, err := os.Stat(absPath); err == nil {
				return absPath, nil
			}
		}

		return ``, fmt.Errorf("invalid entry")
	} else {
		return ``, fmt.Errorf("file %s does not exist", id)
	}
}

func (self *Database) Scan(labels ...string) error {
	defer func() {
		self.ForceRescan = false
	}()

	if self.ScanInProgress {
		log.Warningf("Another scan is already running")
		return fmt.Errorf("Scan already running")
	} else {
		self.ScanInProgress = true
		defer func() {
			self.ScanInProgress = false
		}()
	}

	// get this before performing the scan so that all scanned files will necessarily
	// be greater than it, except for DST/leap second events that happen at *just* the right
	// time, at which point this is still harmless, but I wanted you to know I thought of that :)
	minLastSeen := time.Now().UnixNano()

	var scannedDirectories []Directory

	if err := ScannedDirectories.All(&scannedDirectories); err == nil {
		for _, directory := range scannedDirectories {
			if len(labels) > 0 {
				skip := true

				for _, label := range labels {
					if directory.Label == stringutil.Underscore(label) {
						skip = false
						break
					}
				}

				if skip {
					log.Debugf("Skipping directory %s [%s]", directory.Path, directory.Label)
					continue
				}
			}

			if err := directory.Initialize(self); err == nil {
				log.Debugf("Scanning directory %s [%s]", directory.Path, directory.Label)
				if err := directory.Scan(); err != nil {
					return err
				}
			} else {
				return err
			}
		}
	} else {
		return err
	}

	return self.PropertySet(`metadata.last_scan`, minLastSeen)
}

func (self *Database) setupSchemata() error {
	AuthorizedPeers = mapper.NewModel(self.db, AuthorizedPeersSchema)
	Downloads = mapper.NewModel(self.db, DownloadsSchema)
	Metadata = mapper.NewModel(self.db, MetadataSchema)
	ScannedDirectories = mapper.NewModel(self.db, ScannedDirectoriesSchema)
	Shares = mapper.NewModel(self.db, SharesSchema)
	Subscriptions = mapper.NewModel(self.db, SubscriptionsSchema)
	System = mapper.NewModel(self.db, SystemSchema)

	models := []*mapper.Model{
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
