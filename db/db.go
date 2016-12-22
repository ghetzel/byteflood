package db

import (
	"fmt"
	"github.com/ghetzel/go-stockutil/stringutil"
	"github.com/ghetzel/pivot"
	"github.com/ghetzel/pivot/backends"
	"github.com/ghetzel/pivot/dal"
	"github.com/ghetzel/pivot/filter"
	"github.com/op/go-logging"
	"time"
)

var log = logging.MustGetLogger(`byteflood/scanner`)

var DefaultMetadataCollectionName = `metadata`
var DefaultSystemCollectionName = `byteflood`

type Database struct {
	Directories            []*Directory `json:"directories,omitempty"`
	URI                    string       `json:"uri,omitempty"`
	MetadataCollectionName string       `json:"metadata_collection_name,omitempty"`
	SystemCollectionName   string       `json:"system_collection_name,omitempty"`
	db                     backends.Backend
}

func NewDatabase() *Database {
	return &Database{
		Directories: make([]*Directory, 0),
		URI:         `boltdb:///~/.local/share/byteflood/db`,
		MetadataCollectionName: DefaultMetadataCollectionName,
		SystemCollectionName:   DefaultSystemCollectionName,
	}
}

// Initialize the Database by opening the underlying database
func (self *Database) Initialize() error {
	if db, err := pivot.NewDatabase(self.URI); err == nil {
		self.db = db
	} else {
		return err
	}

	for _, directory := range self.Directories {
		if err := directory.Initialize(self); err != nil {
			return err
		}
	}

	return nil
}

// Add a directory to be scanned
func (self *Database) AddDirectory(directory *Directory) error {
	if err := directory.Initialize(self); err == nil {
		self.Directories = append(self.Directories, directory)
		return nil
	} else {
		return err
	}
}

func (self *Database) ParseFilter(filterString string) (filter.Filter, error) {
	return filter.Parse(filterString)
}

// Query records in the given collection
func (self *Database) Query(collectionName string, f filter.Filter) (*dal.RecordSet, error) {
	if index := self.db.WithSearch(); index != nil {
		return index.Query(collectionName, f)
	} else {
		return nil, fmt.Errorf("Backend type %T does not support searching", self.db)
	}
}

// Return whethere a metadata record with the given ID exists
func (self *Database) RecordExists(id string) bool {
	return self.db.Exists(self.MetadataCollectionName, id)
}

// Retrieve a metadata record by ID
func (self *Database) RetrieveRecord(id string) (map[string]interface{}, error) {
	if record, err := self.db.Retrieve(self.MetadataCollectionName, id); err == nil {
		return record.Fields, nil
	} else {
		return nil, err
	}
}

// Save a given metadata record
func (self *Database) PersistRecord(id string, data map[string]interface{}) error {
	return self.db.Insert(self.MetadataCollectionName, dal.NewRecordSet(
		dal.NewRecord(id).SetFields(data),
	))
}

// Delete metadata records that match the given set of IDs
func (self *Database) DeleteRecords(ids ...string) error {
	return self.db.Delete(self.MetadataCollectionName, ids)
}

// Query records from the metadata collection
func (self *Database) QueryMetadata(filterString string) (*dal.RecordSet, error) {
	if f, err := self.ParseFilter(filterString); err == nil {
		return self.Query(self.MetadataCollectionName, f)
	} else {
		return nil, err
	}
}

// Query records from the system data collection
func (self *Database) QuerySystem(filterString string) (*dal.RecordSet, error) {
	if f, err := self.ParseFilter(filterString); err == nil {
		return self.Query(self.SystemCollectionName, f)
	} else {
		return nil, err
	}
}

// Removes records from the database that would not be added by the current Database instance.
func (self *Database) CleanRecords() error {
	var minLastSeen int64

	if v := self.PropertyGet(`metadata.last_scan`); v != nil {
		if vI, ok := v.(int64); ok {
			minLastSeen = vI
		}
	}

	if minLastSeen <= 0 {
		return fmt.Errorf("Invalid last_scan time")
	}

	if staleRecordSet, err := self.QuerySystem(
		fmt.Sprintf("key/prefix:metadata.last_scan./value/lt:%d", minLastSeen),
	); err == nil {
		ids := make([]string, 0)

		for _, record := range staleRecordSet.Records {
			if v := record.Get(`value`); v != nil {
				if vStr, err := stringutil.ToString(v); err == nil {
					ids = append(ids, vStr)
				}
			}
		}

		log.Debugf("Cleaning up %d records", len(ids))

		return self.DeleteRecords(ids...)
	} else {
		return err
	}
}

func (self *Database) PropertySet(key string, value interface{}, fields ...map[string]interface{}) error {
	record := dal.NewRecord(key).Set(`key`, key).Set(`value`, value)

	if len(fields) > 0 {
		record.SetFields(fields[0])
	}

	return self.db.Insert(self.SystemCollectionName, dal.NewRecordSet(record))
}

func (self *Database) PropertyGet(key string, fallback ...interface{}) interface{} {
	if record, err := self.db.Retrieve(self.SystemCollectionName, key); err == nil {
		return record.Get(`value`, fallback...)
	} else {
		if len(fallback) > 0 {
			return fallback[0]
		} else {
			return nil
		}
	}
}

func (self *Database) Scan(labels ...string) error {
	// get this before performing the scan so that all scanned files will necessarily
	// be greater than it
	minLastSeen := time.Now().UnixNano()

	for _, directory := range self.Directories {
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

		log.Debugf("Scanning directory %s [%s]", directory.Path, directory.Label)
		if err := directory.Scan(); err != nil {
			return err
		}
	}

	return self.PropertySet(`metadata.last_scan`, minLastSeen)
}
